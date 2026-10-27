//go:build testing

package agent

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDockerExec speaks enough of the Docker API for the console: it creates an exec, upgrades
// the start request to a raw stream that upper-cases what it receives, and reports exit code 3
// once the stream gets "exit".
type fakeDockerExec struct {
	listener net.Listener
	mu       sync.Mutex
	created  string
	resizes  []string
}

func newFakeDockerExec(t *testing.T) *fakeDockerExec {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	fake := &fakeDockerExec{listener: listener}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go fake.serve(conn)
		}
	}()
	return fake
}

func (f *fakeDockerExec) manager() *dockerManager {
	return &dockerManager{client: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", f.listener.Addr().String())
		},
	}}}
}

func (f *fakeDockerExec) serve(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	for {
		req, err := http.ReadRequest(reader)
		if err != nil {
			return
		}
		body, _ := io.ReadAll(req.Body)
		path := req.URL.Path
		switch {
		case req.Method == http.MethodPost && strings.HasSuffix(path, "/exec"):
			f.mu.Lock()
			f.created = string(body)
			f.mu.Unlock()
			writeJSON(conn, `{"Id":"exec1"}`)
		case req.Method == http.MethodPost && path == "/exec/exec1/resize":
			f.mu.Lock()
			f.resizes = append(f.resizes, req.URL.RawQuery)
			f.mu.Unlock()
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n")
		case req.Method == http.MethodGet && path == "/exec/exec1/json":
			writeJSON(conn, `{"Running":false,"ExitCode":3}`)
		case req.Method == http.MethodPost && path == "/exec/exec1/start":
			io.WriteString(conn, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			buf := make([]byte, 1024)
			for {
				n, err := reader.Read(buf)
				if n > 0 {
					chunk := string(buf[:n])
					conn.Write([]byte(strings.ToUpper(chunk)))
					if strings.HasPrefix(chunk, "exit") {
						return
					}
				}
				if err != nil {
					return
				}
			}
		default:
			io.WriteString(conn, "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\n\r\n")
		}
	}
}

func writeJSON(conn net.Conn, body string) {
	io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: "+
		strconv.Itoa(len(body))+"\r\n\r\n"+body)
}

// readUntil collects data frames until the output contains want, or returns the exit frame.
func readUntil(t *testing.T, decoder *cbor.Decoder, want string) (output string, exit *common.ExecFrame) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !strings.Contains(output, want) && exit == nil {
			var frame common.ExecFrame
			if err := decoder.Decode(&frame); err != nil {
				return
			}
			switch frame.Kind {
			case common.ExecData:
				output += string(frame.Data)
			case common.ExecExit:
				exit = &frame
			}
		}
	}()
	select {
	case <-done:
	case <-deadline:
		t.Fatalf("timed out waiting for %q, got %q", want, output)
	}
	return output, exit
}

func TestExecPump(t *testing.T) {
	fake := newFakeDockerExec(t)
	dm := fake.manager()

	session, err := dm.startExec(context.Background(), "abcdef123456", 80, 24)
	require.NoError(t, err)
	defer session.conn.Close()
	assert.Contains(t, fake.created, `"Tty":true`)
	assert.Contains(t, fake.created, "TERM=xterm-256color")
	assert.Contains(t, fake.resizes, "h=24&w=80")

	hub, agentSide := net.Pipe()
	defer hub.Close()
	result := make(chan error, 1)
	go func() { result <- session.pump(context.Background(), agentSide, time.Minute) }()

	encoder, decoder := cbor.NewEncoder(hub), cbor.NewDecoder(hub)
	require.NoError(t, encoder.Encode(common.ExecFrame{Kind: common.ExecData, Data: []byte("hello\n")}))
	output, _ := readUntil(t, decoder, "HELLO")
	assert.Contains(t, output, "HELLO")

	require.NoError(t, encoder.Encode(common.ExecFrame{Kind: common.ExecResize, Cols: 100, Rows: 40}))
	require.NoError(t, encoder.Encode(common.ExecFrame{Kind: common.ExecPing}))
	require.NoError(t, encoder.Encode(common.ExecFrame{Kind: common.ExecData, Data: []byte("exit\n")}))
	_, exit := readUntil(t, decoder, "never appears")
	require.NotNil(t, exit)
	assert.Equal(t, common.ExecExit, exit.Kind)
	assert.Equal(t, 3, exit.Code)
	assert.NoError(t, <-result)
	assert.Contains(t, fake.resizes, "h=40&w=100")
}

func TestExecPumpEndsWhenHubDisconnects(t *testing.T) {
	fake := newFakeDockerExec(t)
	session, err := fake.manager().startExec(context.Background(), "abcdef123456", 0, 0)
	require.NoError(t, err)
	defer session.conn.Close()

	hub, agentSide := net.Pipe()
	result := make(chan error, 1)
	go func() { result <- session.pump(context.Background(), agentSide, time.Minute) }()
	hub.Close()
	select {
	case err := <-result:
		assert.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("pump did not stop after the hub disconnected")
	}
}

func TestExecPumpIdleTimeout(t *testing.T) {
	fake := newFakeDockerExec(t)
	session, err := fake.manager().startExec(context.Background(), "abcdef123456", 0, 0)
	require.NoError(t, err)
	defer session.conn.Close()

	hub, agentSide := net.Pipe()
	defer hub.Close()
	go io.Copy(io.Discard, hub)
	err = session.pump(context.Background(), agentSide, 1500*time.Millisecond)
	assert.ErrorContains(t, err, "idle")
}

func execContext(t *testing.T, agent *Agent, req common.ExecRequest, stream io.ReadWriter) *HandlerContext {
	t.Helper()
	data, err := cbor.Marshal(req)
	require.NoError(t, err)
	return &HandlerContext{
		Agent:        agent,
		Request:      &common.HubRequest[cbor.RawMessage]{Action: common.Exec, Data: data},
		Stream:       stream,
		SendResponse: func(any, *uint32) error { return nil },
	}
}

func TestExecHandlerRefusals(t *testing.T) {
	stream, _ := net.Pipe()
	agent := &Agent{dockerManager: newFakeDockerExec(t).manager()}
	req := common.ExecRequest{ContainerID: "abcdef123456"}

	t.Setenv("FORK_ACTIONS", "control") // control does not include the console
	assert.ErrorContains(t, (&ExecHandler{}).Handle(execContext(t, agent, req, stream)), "disabled")

	t.Setenv("FORK_ACTIONS", "exec")
	assert.ErrorContains(t, (&ExecHandler{}).Handle(execContext(t, agent, req, nil)), "SSH")
	assert.ErrorContains(t, (&ExecHandler{}).Handle(execContext(t, &Agent{}, req, stream)), "docker is not available")
	for _, bad := range []string{"", "../x", "web", "zzzzzzzzzzzz"} {
		assert.Error(t, (&ExecHandler{}).Handle(execContext(t, agent, common.ExecRequest{ContainerID: bad}, stream)), bad)
	}

	execSessions.Store(execMaxSessions)
	defer execSessions.Store(0)
	assert.ErrorContains(t, (&ExecHandler{}).Handle(execContext(t, agent, req, stream)), "too many")
	assert.EqualValues(t, execMaxSessions, execSessions.Load(), "a refused session does not leak a slot")
}

func TestParseForkActionsExec(t *testing.T) {
	assert.Equal(t, []string{"control", "exec"}, parseForkActions("control, exec"))
	assert.Equal(t, []string{"exec"}, parseForkActions("EXEC,exec"))
}
