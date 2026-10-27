package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub/expirymap"
	"github.com/henrygd/beszel/internal/hub/systems"
	"github.com/lxzan/gws"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// The console runs in two steps because a browser cannot set headers on a WebSocket: an admin
// asks for a short-lived single-use ticket (an authenticated POST, which is where every check
// happens), then opens the WebSocket with that ticket. The hub relays the terminal between the
// browser and the agent's SSH session.
const (
	actionExec        = "exec" // agent capability (FORK_ACTIONS=exec)
	execTicketTTL     = 30 * time.Second
	execMaxSessions   = 10
	execPingInterval  = 25 * time.Second
	execMaxTerminalSz = 500
)

type execTicket struct {
	UserID      string
	UserEmail   string
	SystemID    string
	SystemName  string
	ContainerID string
}

var (
	execTickets      = expirymap.New[execTicket](time.Minute)
	execTicketMu     sync.Mutex
	execOpenSessions atomic.Int32
)

// execStream is the agent side of a console. systems.ExecStream implements it.
type execStream interface {
	Send(common.ExecFrame) error
	Recv() (common.ExecFrame, error)
	Close()
}

// forkOpenExec opens the console on the agent. It is a variable so tests can stand in for one.
var forkOpenExec = func(sys *systems.System, req common.ExecRequest) (execStream, error) {
	return sys.OpenExec(context.Background(), req)
}

// takeExecTicket returns a ticket once: it is gone after the first successful call.
func takeExecTicket(id string) (execTicket, bool) {
	execTicketMu.Lock()
	defer execTicketMu.Unlock()
	ticket, ok := execTickets.GetOk(id)
	if ok {
		execTickets.Remove(id)
	}
	return ticket, ok
}

// postExecTicket checks that the admin may open a console on the container and hands out a ticket.
// POST /api/beszel/fork/exec-ticket {system, container}
func (h *Hub) postExecTicket(e *core.RequestEvent) error {
	var body struct {
		System    string `json:"system"`
		Container string `json:"container"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if !containerIDPattern.MatchString(body.Container) {
		return e.BadRequestError("Invalid container", nil)
	}
	system, err := h.sm.GetSystem(body.System)
	if err != nil || !system.HasUser(e.App, e.Auth) {
		return e.NotFoundError("", nil)
	}
	record, err := e.App.FindRecordById("systems", body.System)
	if err != nil {
		return e.NotFoundError("", nil)
	}
	entry := auditEntry{
		userID: e.Auth.Id, userEmail: e.Auth.GetString("email"), systemID: body.System, systemName: record.GetString("name"),
		body: controlBody{System: body.System, Kind: "container", Target: body.Container, Action: "exec"},
	}
	if !agentAllows(record, actionExec) {
		entry.result, entry.message = "refused", "the console is not enabled on this system"
		writeAudit(e.App, entry)
		return e.ForbiddenError("The console is not enabled on this system (set FORK_ACTIONS=exec on the agent).", nil)
	}
	id := security.RandomString(32)
	execTickets.Set(id, execTicket{
		UserID: entry.userID, UserEmail: entry.userEmail, SystemID: body.System, SystemName: entry.systemName, ContainerID: body.Container,
	}, execTicketTTL)
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, map[string]string{"ticket": id})
}

var (
	execUpgrader     *gws.Upgrader
	execUpgraderOnce sync.Once
)

func getExecUpgrader() *gws.Upgrader {
	execUpgraderOnce.Do(func() {
		execUpgrader = gws.NewUpgrader(&execWsHandler{}, &gws.ServerOption{})
	})
	return execUpgrader
}

// execSession ties one browser WebSocket to one agent stream.
type execSession struct {
	ready  chan struct{} // closed once the stream is open or has failed
	stream execStream    // nil if opening failed
	closed chan struct{}
	once   sync.Once
}

func (s *execSession) close() {
	s.once.Do(func() {
		close(s.closed)
		<-s.ready
		if s.stream != nil {
			s.stream.Close()
		}
	})
}

// getExec upgrades the request to a WebSocket and relays a console to the agent.
// GET /api/beszel/fork/exec?ticket=...&cols=...&rows=...
func (h *Hub) getExec(e *core.RequestEvent) error {
	query := e.Request.URL.Query()
	ticket, ok := takeExecTicket(query.Get("ticket"))
	if !ok {
		return e.UnauthorizedError("Invalid or expired ticket", nil)
	}
	system, err := h.sm.GetSystem(ticket.SystemID)
	if err != nil {
		return e.NotFoundError("", nil)
	}
	if execOpenSessions.Add(1) > execMaxSessions {
		execOpenSessions.Add(-1)
		return e.JSON(http.StatusTooManyRequests, map[string]string{"error": "too many open consoles"})
	}
	conn, err := getExecUpgrader().Upgrade(e.Response, e.Request)
	if err != nil {
		execOpenSessions.Add(-1)
		return nil // the upgrader already wrote the response
	}
	size := func(name string) uint16 {
		n, _ := strconv.Atoi(query.Get(name))
		if n < 1 || n > execMaxTerminalSz {
			return 0
		}
		return uint16(n)
	}
	session := &execSession{ready: make(chan struct{}), closed: make(chan struct{})}
	conn.Session().Store("exec", session)
	go conn.ReadLoop()
	go h.runExec(e.App, conn, session, system, ticket, common.ExecRequest{ContainerID: ticket.ContainerID, Cols: size("cols"), Rows: size("rows")})
	return nil
}

func (h *Hub) runExec(app core.App, conn *gws.Conn, session *execSession, system *systems.System, ticket execTicket, req common.ExecRequest) {
	defer execOpenSessions.Add(-1)
	entry := auditEntry{
		userID: ticket.UserID, userEmail: ticket.UserEmail, systemID: ticket.SystemID, systemName: ticket.SystemName,
		body: controlBody{System: ticket.SystemID, Kind: "container", Target: ticket.ContainerID, Action: "exec"},
	}
	stream, err := forkOpenExec(system, req)
	if err != nil {
		close(session.ready)
		entry.result, entry.message = "error", err.Error()
		writeAudit(app, entry)
		sendExecText(conn, map[string]any{"type": "error", "message": err.Error()})
		_ = conn.WriteClose(1011, nil)
		return
	}
	session.stream = stream
	close(session.ready)
	defer session.close()

	started := time.Now()
	entry.result, entry.message = "started", ""
	writeAudit(app, entry)
	defer func() {
		entry.result, entry.message = "ended", "after "+time.Since(started).Round(time.Second).String()
		writeAudit(app, entry)
	}()

	// keep the SSH connection and the browser connection open while nobody types
	go func() {
		ticker := time.NewTicker(execPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-session.closed:
				return
			case <-ticker.C:
				if stream.Send(common.ExecFrame{Kind: common.ExecPing}) != nil {
					return
				}
				_ = conn.WritePing(nil)
			}
		}
	}()

	for {
		frame, err := stream.Recv()
		if err != nil {
			_ = conn.WriteClose(1000, nil)
			return
		}
		switch frame.Kind {
		case common.ExecData:
			if conn.WriteMessage(gws.OpcodeBinary, frame.Data) != nil {
				return
			}
		case common.ExecExit:
			sendExecText(conn, map[string]any{"type": "exit", "code": frame.Code})
			_ = conn.WriteClose(1000, nil)
			return
		}
	}
}

func sendExecText(conn *gws.Conn, payload map[string]any) {
	data, _ := json.Marshal(payload)
	_ = conn.WriteMessage(gws.OpcodeText, data)
}

// execWsHandler receives what the browser types: binary messages are terminal input, text
// messages are JSON control messages ({"type":"resize","cols":80,"rows":24}).
type execWsHandler struct{ gws.BuiltinEventHandler }

func (h *execWsHandler) sessionOf(conn *gws.Conn) *execSession {
	value, ok := conn.Session().Load("exec")
	if !ok {
		return nil
	}
	session, _ := value.(*execSession)
	return session
}

func (h *execWsHandler) OnMessage(conn *gws.Conn, message *gws.Message) {
	defer message.Close()
	session := h.sessionOf(conn)
	if session == nil {
		return
	}
	<-session.ready
	if session.stream == nil {
		return
	}
	switch message.Opcode {
	case gws.OpcodeBinary:
		data := append([]byte(nil), message.Data.Bytes()...)
		_ = session.stream.Send(common.ExecFrame{Kind: common.ExecData, Data: data})
	case gws.OpcodeText:
		var control struct {
			Type string `json:"type"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}
		if json.Unmarshal(message.Data.Bytes(), &control) != nil || control.Type != "resize" {
			return
		}
		if control.Cols > 0 && control.Rows > 0 && control.Cols <= execMaxTerminalSz && control.Rows <= execMaxTerminalSz {
			_ = session.stream.Send(common.ExecFrame{Kind: common.ExecResize, Cols: control.Cols, Rows: control.Rows})
		}
	}
}

func (h *execWsHandler) OnClose(conn *gws.Conn, _ error) {
	if session := h.sessionOf(conn); session != nil {
		go session.close()
	}
}
