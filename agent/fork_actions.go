package agent

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/agent/utils"
	"github.com/henrygd/beszel/internal/common"
)

// Actions are off by default: the agent only changes anything on its host when the operator
// opts in with FORK_ACTIONS. "control" allows starting, stopping and restarting containers
// and systemd services. The enabled actions are reported to the hub in the system info.
const (
	forkActionControl = "control"
	forkActionExec    = "exec" // interactive shell in a container, see fork_exec.go
)

// controlTimeout bounds one start, stop or restart. Stopping waits for the container's own grace period.
const controlTimeout = 60 * time.Second

var controlActions = []string{"start", "stop", "restart"}

// parseForkActions returns the known actions listed in a FORK_ACTIONS value.
func parseForkActions(value string) []string {
	var enabled []string
	for entry := range strings.SplitSeq(value, ",") {
		entry = strings.ToLower(strings.TrimSpace(entry))
		if (entry == forkActionControl || entry == forkActionExec) && !slices.Contains(enabled, entry) {
			enabled = append(enabled, entry)
		}
	}
	return enabled
}

// forkActionsEnabled returns the actions enabled by the operator. It is read on every call so
// the handler never trusts state from before a restart.
func forkActionsEnabled() []string {
	value, _ := utils.GetEnv("FORK_ACTIONS")
	return parseForkActions(value)
}

// ControlHandler starts, stops or restarts a container or a systemd service.
type ControlHandler struct{}

func (h *ControlHandler) Handle(hctx *HandlerContext) error {
	if !slices.Contains(forkActionsEnabled(), forkActionControl) {
		return errors.New("actions are disabled on this agent (set FORK_ACTIONS=control)")
	}
	var req common.ControlRequest
	if err := cbor.Unmarshal(hctx.Request.Data, &req); err != nil {
		return err
	}
	if !slices.Contains(controlActions, req.Action) {
		return fmt.Errorf("unknown action %q", req.Action)
	}

	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()

	var result string
	var err error
	switch req.Kind {
	case "container":
		if hctx.Agent.dockerManager == nil {
			return errors.New("docker is not available on this agent")
		}
		result, err = hctx.Agent.dockerManager.controlContainer(ctx, req.Target, req.Action)
	case "service":
		if hctx.Agent.systemdManager == nil {
			return errors.New("systemd is not available on this agent")
		}
		result, err = hctx.Agent.systemdManager.controlService(ctx, req.Target, req.Action)
	default:
		return fmt.Errorf("unknown kind %q", req.Kind)
	}
	if err != nil {
		return err
	}
	return hctx.SendResponse(result, hctx.RequestID)
}

// controlContainer starts, stops or restarts a container through the Docker API.
// Docker answers 204 on success and 304 when the container is already in the requested state.
func (dm *dockerManager) controlContainer(ctx context.Context, containerID, action string) (string, error) {
	endpoint, err := buildDockerContainerEndpoint(containerID, action, nil)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return "", err
	}
	// the shared client has a short timeout meant for stats; stop and restart wait for the grace period
	client := &http.Client{Transport: dm.client.Transport}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return "ok", nil
	case http.StatusNotModified:
		return "already " + map[string]string{"start": "running", "stop": "stopped"}[action], nil
	case http.StatusNotFound:
		return "", errors.New("no such container")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("%s failed: %s: %s", action, resp.Status, strings.TrimSpace(string(body)))
	}
}
