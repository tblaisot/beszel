//go:build testing

package agent

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseForkActions(t *testing.T) {
	assert.Empty(t, parseForkActions(""))
	assert.Equal(t, []string{"control"}, parseForkActions("control"))
	assert.Equal(t, []string{"control", "exec"}, parseForkActions(" Control , control, exec, nonsense"))
	assert.Equal(t, []string{"exec"}, parseForkActions("exec"))
	assert.Empty(t, parseForkActions("shell,reboot"))
}

func controlContext(t *testing.T, agent *Agent, req common.ControlRequest, sent *string) *HandlerContext {
	t.Helper()
	data, err := cbor.Marshal(req)
	require.NoError(t, err)
	return &HandlerContext{
		Agent:   agent,
		Request: &common.HubRequest[cbor.RawMessage]{Action: common.Control, Data: data},
		SendResponse: func(data any, _ *uint32) error {
			*sent = data.(string)
			return nil
		},
	}
}

func TestControlHandlerDisabledByDefault(t *testing.T) {
	t.Setenv("FORK_ACTIONS", "")
	var sent string
	ctx := controlContext(t, &Agent{}, common.ControlRequest{Kind: "container", Target: "abcdef123456", Action: "stop"}, &sent)
	err := (&ControlHandler{}).Handle(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
	assert.Empty(t, sent)
}

func TestControlHandlerValidation(t *testing.T) {
	t.Setenv("FORK_ACTIONS", "control")
	var sent string
	for name, req := range map[string]common.ControlRequest{
		"unknown action":      {Kind: "container", Target: "abcdef123456", Action: "kill"},
		"unknown kind":        {Kind: "volume", Target: "abcdef123456", Action: "stop"},
		"no docker":           {Kind: "container", Target: "abcdef123456", Action: "stop"},
		"no systemd":          {Kind: "service", Target: "web.service", Action: "restart"},
		"empty target action": {Kind: "service", Target: "web.service", Action: ""},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Error(t, (&ControlHandler{}).Handle(controlContext(t, &Agent{}, req, &sent)))
			assert.Empty(t, sent)
		})
	}
}

// dockerFake answers Docker API calls for any host name, as the agent's unix socket client would.
func dockerFake(t *testing.T, handler http.HandlerFunc) *dockerManager {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &dockerManager{client: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
		},
	}}}
}

func TestControlContainer(t *testing.T) {
	const id = "abcdef123456"
	var calls []string
	dm := dockerFake(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/containers/" + id + "/restart", "/containers/" + id + "/stop":
			w.WriteHeader(http.StatusNoContent)
		case "/containers/" + id + "/start":
			w.WriteHeader(http.StatusNotModified)
		case "/containers/0123456789ab/stop":
			w.WriteHeader(http.StatusNotFound)
		default:
			http.Error(w, "boom", http.StatusInternalServerError)
		}
	})
	ctx := context.Background()

	result, err := dm.controlContainer(ctx, id, "restart")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)

	result, err = dm.controlContainer(ctx, id, "start")
	require.NoError(t, err)
	assert.Equal(t, "already running", result)

	_, err = dm.controlContainer(ctx, "0123456789ab", "stop")
	assert.ErrorContains(t, err, "no such container")

	_, err = dm.controlContainer(ctx, "aaaaaaaaaaaa", "stop")
	assert.ErrorContains(t, err, "500")

	// anything that is not a container id never reaches Docker
	calls = nil
	for _, bad := range []string{"", "../images", "web", "abc/def", "zzzzzzzzzzzz"} {
		_, err = dm.controlContainer(ctx, bad, "stop")
		assert.Error(t, err, bad)
	}
	assert.Empty(t, calls)
	assert.Equal(t, 0, len(calls))
}
