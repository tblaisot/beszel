package agent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/henrygd/beszel/internal/common"
)

// The console runs a shell in a container through the Docker exec API and carries it over the
// SSH session that made the request: the hub sends an ExecRequest, the agent answers "ready",
// and from then on both sides exchange common.ExecFrames until the shell or the hub goes away.
// It needs FORK_ACTIONS=exec, and only works for hubs that connect to the agent over SSH.
const (
	execMaxSessions = 4
	execMaxDuration = 2 * time.Hour
	execIdleTimeout = 15 * time.Minute
)

var execSessions atomic.Int32

// ExecHandler opens an interactive shell in a container.
type ExecHandler struct{}

func (h *ExecHandler) Handle(hctx *HandlerContext) error {
	if !slices.Contains(forkActionsEnabled(), forkActionExec) {
		return errors.New("the console is disabled on this agent (set FORK_ACTIONS=exec)")
	}
	if hctx.Stream == nil {
		return errors.New("the console needs the SSH connection to the agent")
	}
	var req common.ExecRequest
	if err := cbor.Unmarshal(hctx.Request.Data, &req); err != nil {
		return err
	}
	dm := hctx.Agent.dockerManager
	if dm == nil {
		return errors.New("docker is not available on this agent")
	}
	if err := validateContainerID(req.ContainerID); err != nil {
		return err
	}
	if execSessions.Add(1) > execMaxSessions {
		execSessions.Add(-1)
		return errors.New("too many console sessions on this agent")
	}
	defer execSessions.Add(-1)

	ctx, cancel := context.WithTimeout(context.Background(), execMaxDuration)
	defer cancel()
	session, err := dm.startExec(ctx, req.ContainerID, req.Cols, req.Rows)
	if err != nil {
		return err
	}
	defer func() { _ = session.conn.Close() }()
	if err := hctx.SendResponse("ready", hctx.RequestID); err != nil {
		return err
	}
	return session.pump(ctx, hctx.Stream, execIdleTimeout)
}

// dockerExec is a running exec instance with its raw terminal stream.
type dockerExec struct {
	dm     *dockerManager
	id     string
	conn   net.Conn
	reader io.Reader // conn, behind the buffered reader that parsed the upgrade response
}

// execClient is the Docker client for exec calls: the shared one has a 2 s timeout meant for stats.
func (dm *dockerManager) execClient() *http.Client {
	return &http.Client{Transport: dm.client.Transport}
}

func (dm *dockerManager) execJSON(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := dm.execClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// dialDocker opens a raw connection to the Docker API, the way the shared client does.
func (dm *dockerManager) dialDocker(ctx context.Context) (net.Conn, error) {
	rt := dm.client.Transport
	if wrapped, ok := rt.(*userAgentRoundTripper); ok {
		rt = wrapped.rt
	}
	transport, ok := rt.(*http.Transport)
	if !ok || transport.DialContext == nil {
		return nil, errors.New("cannot open a raw connection to docker")
	}
	return transport.DialContext(ctx, "tcp", "localhost:80")
}

// startExec creates an exec instance running a shell with a terminal and attaches to it.
func (dm *dockerManager) startExec(ctx context.Context, containerID string, cols, rows uint16) (*dockerExec, error) {
	var created struct {
		Id string `json:"Id"`
	}
	err := dm.execJSON(ctx, http.MethodPost, "/containers/"+containerID+"/exec", map[string]any{
		"AttachStdin": true, "AttachStdout": true, "AttachStderr": true, "Tty": true,
		"Env": []string{"TERM=xterm-256color"},
		// bash when the image has it, plain sh otherwise
		"Cmd": []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"},
	}, &created)
	if err != nil {
		return nil, err
	}
	if created.Id == "" {
		return nil, errors.New("docker returned no exec id")
	}

	conn, err := dm.dialDocker(ctx)
	if err != nil {
		return nil, err
	}
	payload := []byte(`{"Detach":false,"Tty":true}`)
	request := fmt.Sprintf("POST /exec/%s/start HTTP/1.1\r\nHost: localhost\r\nContent-Type: application/json\r\n"+
		"Connection: Upgrade\r\nUpgrade: tcp\r\nContent-Length: %d\r\n\r\n%s", created.Id, len(payload), payload)
	if _, err := io.WriteString(conn, request); err != nil {
		_ = conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, nil)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	// Docker upgrades the connection with 101; some engines answer 200 and stream the terminal.
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = conn.Close()
		return nil, fmt.Errorf("exec start: %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	session := &dockerExec{dm: dm, id: created.Id, conn: conn, reader: reader}
	if cols > 0 && rows > 0 {
		session.resize(ctx, cols, rows)
	}
	return session, nil
}

func (e *dockerExec) resize(ctx context.Context, cols, rows uint16) {
	_ = e.dm.execJSON(ctx, http.MethodPost, fmt.Sprintf("/exec/%s/resize?h=%d&w=%d", e.id, rows, cols), nil, nil)
}

// exitCode reads the shell's exit code, waiting briefly for Docker to record it.
func (e *dockerExec) exitCode(ctx context.Context) int {
	for range 10 {
		var state struct {
			Running  bool `json:"Running"`
			ExitCode int  `json:"ExitCode"`
		}
		if err := e.dm.execJSON(ctx, http.MethodGet, "/exec/"+e.id+"/json", nil, &state); err == nil && !state.Running {
			return state.ExitCode
		}
		time.Sleep(200 * time.Millisecond)
	}
	return -1
}

// pump moves terminal bytes between the container and the hub until the shell ends, the hub goes
// away, the session has been idle for too long, or ctx ends.
func (e *dockerExec) pump(ctx context.Context, stream io.ReadWriter, idle time.Duration) error {
	var lastActivity atomic.Int64
	lastActivity.Store(time.Now().UnixNano())
	touch := func() { lastActivity.Store(time.Now().UnixNano()) }

	encoder := cbor.NewEncoder(stream)
	var writeMu sync.Mutex
	send := func(frame common.ExecFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(frame)
	}

	shellDone := make(chan struct{})
	go func() { // container output to the hub
		defer close(shellDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := e.reader.Read(buf)
			if n > 0 {
				touch()
				if sendErr := send(common.ExecFrame{Kind: common.ExecData, Data: buf[:n]}); sendErr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	hubGone := make(chan struct{})
	go func() { // hub input to the container
		defer close(hubGone)
		decoder := cbor.NewDecoder(stream)
		for {
			var frame common.ExecFrame
			if err := decoder.Decode(&frame); err != nil {
				return
			}
			// pings only keep the connection open; they must not keep an unused shell alive
			switch frame.Kind {
			case common.ExecData:
				touch()
				if _, err := e.conn.Write(frame.Data); err != nil {
					return
				}
			case common.ExecResize:
				touch()
				if frame.Cols > 0 && frame.Rows > 0 {
					e.resize(ctx, frame.Cols, frame.Rows)
				}
			}
		}
	}()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-shellDone:
			return send(common.ExecFrame{Kind: common.ExecExit, Code: e.exitCode(ctx)})
		case <-hubGone:
			return nil // the deferred close of the connection ends the shell
		case <-ctx.Done():
			_ = send(common.ExecFrame{Kind: common.ExecExit, Code: -1})
			return ctx.Err()
		case <-ticker.C:
			if time.Since(time.Unix(0, lastActivity.Load())) > idle {
				_ = send(common.ExecFrame{Kind: common.ExecExit, Code: -1})
				return errors.New("console closed after being idle")
			}
		}
	}
}
