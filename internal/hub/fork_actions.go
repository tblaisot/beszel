package hub

import (
	"encoding/json"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub/systems"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// actionControl is the agent capability that allows start, stop and restart (FORK_ACTIONS=control).
const actionControl = "control"

var (
	controlActions    = []string{"start", "stop", "restart"}
	serviceNameRegexp = regexp.MustCompile(`^[a-zA-Z0-9:_.@\\-]+$`)
	// units of these types are not services and are never controlled
	otherUnitTypes = []string{".timer", ".socket", ".target", ".mount", ".automount", ".path", ".slice", ".scope", ".swap", ".device"}
	// auditRetention is how long action records are kept.
	auditRetention = 180 * 24 * time.Hour
)

// forkControlAgent sends the request to the agent. It is a variable so tests can stand in for one.
var forkControlAgent = func(sys *systems.System, req common.ControlRequest) (string, error) {
	return sys.ControlOnAgent(req)
}

type controlBody struct {
	System string `json:"system"`
	Kind   string `json:"kind"`   // container or service
	Target string `json:"target"` // container id or service unit name
	Action string `json:"action"` // start, stop or restart
}

type auditEntry struct {
	userID     string
	userEmail  string
	systemID   string
	systemName string
	body       controlBody
	result     string // ok, error or refused
	message    string
}

// postControl starts, stops or restarts a container or service on a system. Admins only
// (enforced by the route), on systems they can read, and only when the agent reports the
// capability. Every attempt that reaches this point is written to the audit log.
// POST /api/beszel/fork/actions
func (h *Hub) postControl(e *core.RequestEvent) error {
	var body controlBody
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("Invalid request body", err)
	}
	if !slices.Contains(controlActions, body.Action) {
		return e.BadRequestError("Invalid action", nil)
	}
	switch body.Kind {
	case "container":
		if !containerIDPattern.MatchString(body.Target) {
			return e.BadRequestError("Invalid container", nil)
		}
	case "service":
		if !validServiceName(body.Target) {
			return e.BadRequestError("Invalid service", nil)
		}
	default:
		return e.BadRequestError("Invalid kind", nil)
	}

	system, err := h.sm.GetSystem(body.System)
	if err != nil || !system.HasUser(e.App, e.Auth) {
		return e.NotFoundError("", nil)
	}
	record, err := e.App.FindRecordById("systems", body.System)
	if err != nil {
		return e.NotFoundError("", nil)
	}
	entry := auditEntry{userID: e.Auth.Id, userEmail: e.Auth.GetString("email"), systemID: body.System, systemName: record.GetString("name"), body: body}

	if !agentAllows(record, actionControl) {
		entry.result, entry.message = "refused", "actions are not enabled on this system"
		writeAudit(e.App, entry)
		return e.ForbiddenError("Actions are not enabled on this system (set FORK_ACTIONS=control on the agent).", nil)
	}

	result, err := forkControlAgent(system, common.ControlRequest{Kind: body.Kind, Target: body.Target, Action: body.Action})
	if err != nil {
		entry.result, entry.message = "error", err.Error()
		writeAudit(e.App, entry)
		return e.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	entry.result, entry.message = "ok", result
	writeAudit(e.App, entry)
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, map[string]string{"result": result})
}

// validServiceName accepts a service name as the hub stores it (without ".service") or with the suffix.
func validServiceName(name string) bool {
	if !serviceNameRegexp.MatchString(name) {
		return false
	}
	for _, suffix := range otherUnitTypes {
		if strings.HasSuffix(name, suffix) {
			return false
		}
	}
	return true
}

// agentAllows reports whether the agent of a system listed the action in its info.
func agentAllows(system *core.Record, action string) bool {
	var info struct {
		Actions []string `json:"fa"`
	}
	if raw := system.GetString("info"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &info)
	}
	return slices.Contains(info.Actions, action)
}

// writeAudit appends one record to fork_audit and drops records past the retention period.
// A failure to write is logged but never blocks the action's response.
func writeAudit(app core.App, entry auditEntry) {
	collection, err := app.FindCachedCollectionByNameOrId("fork_audit")
	if err != nil {
		app.Logger().Error("Audit collection missing", "err", err)
		return
	}
	record := core.NewRecord(collection)
	record.Set("user", entry.userID)
	record.Set("user_email", entry.userEmail)
	record.Set("system", entry.systemID)
	record.Set("system_name", entry.systemName)
	record.Set("kind", entry.body.Kind)
	record.Set("target", entry.body.Target)
	record.Set("action", entry.body.Action)
	record.Set("result", entry.result)
	record.Set("message", entry.message)
	if err := app.Save(record); err != nil {
		app.Logger().Error("Failed to write audit record", "err", err)
		return
	}
	cutoff := time.Now().UTC().Add(-auditRetention).Format("2006-01-02 15:04:05.000Z")
	if _, err := app.DB().NewQuery("DELETE FROM fork_audit WHERE created < {:cutoff}").Bind(dbx.Params{"cutoff": cutoff}).Execute(); err != nil {
		app.Logger().Warn("Failed to prune audit records", "err", err)
	}
}
