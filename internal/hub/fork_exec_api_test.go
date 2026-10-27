//go:build testing

package hub_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub"
	"github.com/henrygd/beszel/internal/hub/systems"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/lxzan/gws"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAgentConsole stands in for the agent: it upper-cases terminal input and ends the shell with
// code 7 when it receives "exit".
type fakeAgentConsole struct {
	mu      sync.Mutex
	out     chan common.ExecFrame
	resizes []common.ExecFrame
	pings   int
	closed  chan struct{}
	once    sync.Once
}

func newFakeAgentConsole() *fakeAgentConsole {
	return &fakeAgentConsole{out: make(chan common.ExecFrame, 16), closed: make(chan struct{})}
}

func (f *fakeAgentConsole) Send(frame common.ExecFrame) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch frame.Kind {
	case common.ExecData:
		f.out <- common.ExecFrame{Kind: common.ExecData, Data: []byte(strings.ToUpper(string(frame.Data)))}
		if strings.HasPrefix(string(frame.Data), "exit") {
			f.out <- common.ExecFrame{Kind: common.ExecExit, Code: 7}
		}
	case common.ExecResize:
		f.resizes = append(f.resizes, frame)
	case common.ExecPing:
		f.pings++
	}
	return nil
}

func (f *fakeAgentConsole) Recv() (common.ExecFrame, error) {
	select {
	case frame := <-f.out:
		return frame, nil
	case <-f.closed:
		return common.ExecFrame{}, io.EOF
	}
}

func (f *fakeAgentConsole) Close() { f.once.Do(func() { close(f.closed) }) }

type wsCollector struct {
	gws.BuiltinEventHandler
	binary chan string
	text   chan string
	closed chan struct{}
}

func (c *wsCollector) OnMessage(_ *gws.Conn, message *gws.Message) {
	defer message.Close()
	data := string(message.Data.Bytes())
	if message.Opcode == gws.OpcodeBinary {
		c.binary <- data
	} else {
		c.text <- data
	}
}

func (c *wsCollector) OnClose(*gws.Conn, error) { close(c.closed) }

func TestExecConsole(t *testing.T) {
	testHub, err := beszelTests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	_ = testHub.StartHub()
	defer testHub.Cleanup()

	router, err := apis.NewRouter(testHub.TestApp)
	require.NoError(t, err)
	var handler http.Handler
	require.NoError(t, testHub.TestApp.OnServe().Trigger(&core.ServeEvent{App: testHub.TestApp, Router: router}, func(e *core.ServeEvent) error {
		var buildErr error
		handler, buildErr = e.Router.BuildMux()
		return buildErr
	}))
	server := httptest.NewServer(handler)
	defer server.Close()

	user, err := beszelTests.CreateUser(testHub, "user@example.com", "password123")
	require.NoError(t, err)
	admin, err := beszelTests.CreateUserWithRole(testHub, "admin@example.com", "password123", "admin")
	require.NoError(t, err)
	outsider, err := beszelTests.CreateUserWithRole(testHub, "outsider@example.com", "password123", "admin")
	require.NoError(t, err)
	userToken, _ := user.NewAuthToken()
	adminToken, _ := admin.NewAuthToken()
	outsiderToken, _ := outsider.NewAuthToken()

	withConsole, err := beszelTests.CreateRecord(testHub, "systems", map[string]any{
		"name": "with-console", "host": "10.0.0.1", "port": "45876", "users": []string{admin.Id, user.Id},
	})
	require.NoError(t, err)
	_, err = testHub.DB().NewQuery("UPDATE systems SET info = {:info} WHERE id = {:id}").
		Bind(dbx.Params{"info": `{"fa":["control","exec"]}`, "id": withConsole.Id}).Execute()
	require.NoError(t, err)
	controlOnly, err := beszelTests.CreateRecord(testHub, "systems", map[string]any{
		"name": "control-only", "host": "10.0.0.2", "port": "45876", "users": []string{admin.Id},
	})
	require.NoError(t, err)
	_, err = testHub.DB().NewQuery("UPDATE systems SET info = {:info} WHERE id = {:id}").
		Bind(dbx.Params{"info": `{"fa":["control"]}`, "id": controlOnly.Id}).Execute()
	require.NoError(t, err)

	postTicket := func(token string, body map[string]any) (int, map[string]string) {
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest(http.MethodPost, server.URL+"/api/beszel/fork/exec-ticket", strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		res, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		defer res.Body.Close()
		var out map[string]string
		_ = json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	container := "abcdef123456"

	t.Run("ticket checks", func(t *testing.T) {
		status, _ := postTicket("", map[string]any{"system": withConsole.Id, "container": container})
		assert.Equal(t, 401, status)
		status, _ = postTicket(userToken, map[string]any{"system": withConsole.Id, "container": container})
		assert.Equal(t, 403, status, "regular users cannot open a console")
		status, _ = postTicket(adminToken, map[string]any{"system": withConsole.Id, "container": "../etc"})
		assert.Equal(t, 400, status)
		status, _ = postTicket(outsiderToken, map[string]any{"system": withConsole.Id, "container": container})
		assert.Equal(t, 404, status, "admin who cannot read the system")
		status, _ = postTicket(adminToken, map[string]any{"system": controlOnly.Id, "container": container})
		assert.Equal(t, 403, status, "agent without the exec capability")
		records, err := testHub.FindAllRecords("fork_audit")
		require.NoError(t, err)
		require.Len(t, records, 1, "only the refused attempt is logged")
		assert.Equal(t, "refused", records[0].GetString("result"))
		assert.Equal(t, "exec", records[0].GetString("action"))
	})

	wsURL := func(ticket string) string {
		u, _ := url.Parse(server.URL)
		return "ws://" + u.Host + "/api/beszel/fork/exec?cols=100&rows=30&ticket=" + url.QueryEscape(ticket)
	}

	t.Run("invalid tickets are refused", func(t *testing.T) {
		res, err := http.Get(strings.Replace(wsURL("nope"), "ws://", "http://", 1))
		require.NoError(t, err)
		res.Body.Close()
		assert.Equal(t, 401, res.StatusCode)
	})

	t.Run("console session", func(t *testing.T) {
		fake := newFakeAgentConsole()
		var opened common.ExecRequest
		restore := hub.SetForkOpenExec(func(_ *systems.System, req common.ExecRequest) (hub.ExecStream, error) {
			opened = req
			return fake, nil
		})
		defer restore()

		status, out := postTicket(adminToken, map[string]any{"system": withConsole.Id, "container": container})
		require.Equal(t, 200, status)
		ticket := out["ticket"]
		require.Len(t, ticket, 32)

		collector := &wsCollector{binary: make(chan string, 8), text: make(chan string, 8), closed: make(chan struct{})}
		client, _, err := gws.NewClient(collector, &gws.ClientOption{Addr: wsURL(ticket)})
		require.NoError(t, err)
		go client.ReadLoop()

		// a ticket works once
		res, err := http.Get(strings.Replace(wsURL(ticket), "ws://", "http://", 1))
		require.NoError(t, err)
		res.Body.Close()
		assert.Equal(t, 401, res.StatusCode)

		require.NoError(t, client.WriteMessage(gws.OpcodeBinary, []byte("hello\n")))
		select {
		case got := <-collector.binary:
			assert.Equal(t, "HELLO\n", got)
		case <-time.After(5 * time.Second):
			t.Fatal("no output from the console")
		}
		assert.Equal(t, common.ExecRequest{ContainerID: container, Cols: 100, Rows: 30}, opened)

		require.NoError(t, client.WriteMessage(gws.OpcodeText, []byte(`{"type":"resize","cols":120,"rows":40}`)))
		require.NoError(t, client.WriteMessage(gws.OpcodeText, []byte(`{"type":"resize","cols":9999,"rows":40}`))) // out of range
		require.NoError(t, client.WriteMessage(gws.OpcodeText, []byte(`not json`)))
		require.NoError(t, client.WriteMessage(gws.OpcodeBinary, []byte("exit\n")))

		select {
		case got := <-collector.binary:
			assert.Equal(t, "EXIT\n", got)
		case <-time.After(5 * time.Second):
			t.Fatal("no echo of exit")
		}
		select {
		case got := <-collector.text:
			assert.JSONEq(t, `{"type":"exit","code":7}`, got)
		case <-time.After(5 * time.Second):
			t.Fatal("no exit message")
		}
		select {
		case <-collector.closed:
		case <-time.After(5 * time.Second):
			t.Fatal("connection was not closed after the shell ended")
		}

		fake.mu.Lock()
		assert.Equal(t, []common.ExecFrame{{Kind: common.ExecResize, Cols: 120, Rows: 40}}, fake.resizes)
		fake.mu.Unlock()

		// started and ended are both logged
		require.Eventually(t, func() bool {
			records, _ := testHub.FindAllRecords("fork_audit")
			results := map[string]bool{}
			for _, r := range records {
				if r.GetString("action") == "exec" {
					results[r.GetString("result")] = true
					assert.Equal(t, "admin@example.com", r.GetString("user_email"))
					assert.Equal(t, container, r.GetString("target"))
				}
			}
			return results["started"] && results["ended"]
		}, 5*time.Second, 50*time.Millisecond)
	})

	t.Run("agent failure is reported", func(t *testing.T) {
		restore := hub.SetForkOpenExec(func(*systems.System, common.ExecRequest) (hub.ExecStream, error) {
			return nil, errors.New("docker is not available on this agent")
		})
		defer restore()
		status, out := postTicket(adminToken, map[string]any{"system": withConsole.Id, "container": container})
		require.Equal(t, 200, status)

		collector := &wsCollector{binary: make(chan string, 8), text: make(chan string, 8), closed: make(chan struct{})}
		client, _, err := gws.NewClient(collector, &gws.ClientOption{Addr: wsURL(out["ticket"])})
		require.NoError(t, err)
		go client.ReadLoop()
		select {
		case got := <-collector.text:
			assert.Contains(t, got, `"type":"error"`)
			assert.Contains(t, got, "docker is not available")
		case <-time.After(5 * time.Second):
			t.Fatal("no error message")
		}
		<-collector.closed
		require.Eventually(t, func() bool {
			record, err := testHub.FindFirstRecordByFilter("fork_audit", "action = 'exec' && result = 'error'")
			return err == nil && strings.Contains(record.GetString("message"), "docker is not available")
		}, 5*time.Second, 50*time.Millisecond)
	})
}
