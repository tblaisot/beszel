package hub

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/henrygd/beszel/internal/entities/systemd"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Severities of an attention item, most serious first.
const (
	severityCritical = "critical"
	severityWarning  = "warning"
	severityInfo     = "info"
)

// attentionItem is one thing that needs a look. Page and Ref tell the UI where it links to.
type attentionItem struct {
	Type     string `json:"type"` // system, monitor, alert, zfs, smart, unit, container, update
	Severity string `json:"severity"`
	System   string `json:"system"` // system id
	Name     string `json:"name"`
	Reason   string `json:"reason"`
	Since    string `json:"since,omitempty"`
}

type attentionFacts struct {
	Systems    []factSystem
	Alerts     []factAlert
	Monitors   []factMonitor
	Pools      []factPool
	Smart      []factSmart
	Units      []factUnit
	Containers []factContainer
	Details    map[string]factDetails
	Now        time.Time
}

type factSystem struct {
	ID      string `db:"id"`
	Name    string `db:"name"`
	Status  string `db:"status"`
	Updated string `db:"updated"`
	Parent  string `db:"parent"`
	Info    string `db:"info"`
}

type factAlert struct {
	System  string  `db:"system"`
	Name    string  `db:"name"`
	Value   float64 `db:"value"`
	Updated string  `db:"updated"`
}

type factMonitor struct {
	System   string  `db:"system"`
	Name     string  `db:"name"`
	Target   string  `db:"target"`
	Protocol string  `db:"protocol"`
	Port     int     `db:"port"`
	Res      float64 `db:"res"`
	Updated  string  `db:"updated"`
}

type factPool struct {
	System string  `db:"system"`
	Name   string  `db:"name"`
	Health string  `db:"health"`
	Alloc  float64 `db:"alloc"`
	Size   float64 `db:"size"`
}

type factSmart struct {
	System string `db:"system"`
	Name   string `db:"name"`
	Model  string `db:"model"`
	State  string `db:"state"`
}

type factUnit struct {
	System string `db:"system"`
	Name   string `db:"name"`
	State  int    `db:"state"`
}

type factContainer struct {
	System    string `db:"system"`
	Name      string `db:"name"`
	Status    string `db:"status"`
	Health    int    `db:"health"`
	Updatable bool   `db:"updatable"`
}

type factDetails struct {
	Cores  float64 `db:"cores"`
	Memory float64 `db:"memory"`
}

type attentionResponse struct {
	Items    []attentionItem   `json:"items"`
	Counts   map[string]int    `json:"counts"`
	Overview attentionOverview `json:"overview"`
}

type attentionOverview struct {
	SystemsUp     int             `json:"systemsUp"`
	SystemsTotal  int             `json:"systemsTotal"`
	MonitorsUp    int             `json:"monitorsUp"`
	MonitorsTotal int             `json:"monitorsTotal"`
	AvgResponseUs float64         `json:"avgResponseUs"` // average response of monitors that are up, microseconds
	UpdatesAvail  int             `json:"updates"`
	Hosts         []attentionHost `json:"hosts"`
	Pools         []attentionPool `json:"pools"`
}

// attentionHost is a system other systems are linked to (their parent host).
type attentionHost struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	CpuPct   float64 `json:"cpu"`
	MemPct   float64 `json:"mp"`
	Cores    float64 `json:"cores"`
	MemBytes float64 `json:"memory"`
}

type attentionPool struct {
	System  string  `json:"system"`
	Name    string  `json:"name"`
	Health  string  `json:"health"`
	UsedPct float64 `json:"usedPct"`
	Size    float64 `json:"size"`
}

// buildAttention turns the current facts into the ranked list of problems and the overview numbers.
func buildAttention(f attentionFacts) attentionResponse {
	systemName := map[string]string{}
	systemUp := map[string]bool{}
	for _, s := range f.Systems {
		systemName[s.ID] = s.Name
		systemUp[s.ID] = s.Status == "up"
	}
	items := []attentionItem{}
	add := func(item attentionItem) { items = append(items, item) }

	for _, s := range f.Systems {
		if s.Status == "down" {
			add(attentionItem{Type: "system", Severity: severityCritical, System: s.ID, Name: s.Name, Reason: "Connection is down", Since: s.Updated})
		}
	}
	for _, a := range f.Alerts {
		// a down system already has its own item
		if a.Name == "Status" {
			continue
		}
		add(attentionItem{Type: "alert", Severity: severityWarning, System: a.System, Name: systemName[a.System], Reason: a.Name + " alert triggered", Since: a.Updated})
	}

	failing, monitorsUp, monitorsTotal := 0, 0, 0
	var responseSum float64
	for _, m := range f.Monitors {
		if !systemUp[m.System] || m.Updated == "" {
			continue // paused or unreachable systems cannot probe, so their monitors say nothing
		}
		monitorsTotal++
		if m.Res > 0 {
			monitorsUp++
			responseSum += m.Res
			continue
		}
		failing++
		name := m.Name
		if name == "" {
			name = m.Target
			if m.Protocol == "tcp" {
				name += ":" + strconv.Itoa(m.Port)
			}
		}
		add(attentionItem{Type: "monitor", Severity: severityCritical, System: m.System, Name: name, Reason: "Not responding (" + strings.ToUpper(m.Protocol) + ")", Since: m.Updated})
	}

	for _, p := range f.Pools {
		if p.Health == "" || p.Health == "ONLINE" {
			continue
		}
		severity := severityCritical
		if p.Health == "DEGRADED" {
			severity = severityWarning
		}
		add(attentionItem{Type: "zfs", Severity: severity, System: p.System, Name: p.Name, Reason: "Pool " + strings.ToLower(p.Health)})
	}
	for _, d := range f.Smart {
		if d.State == "" || d.State == "PASSED" || d.State == "UNKNOWN" {
			continue
		}
		severity := severityWarning
		if d.State == "FAILED" {
			severity = severityCritical
		}
		name := d.Name
		if d.Model != "" {
			name += " (" + d.Model + ")"
		}
		add(attentionItem{Type: "smart", Severity: severity, System: d.System, Name: name, Reason: "S.M.A.R.T. " + strings.ToLower(d.State)})
	}
	for _, u := range f.Units {
		if systemd.ServiceState(u.State) == systemd.StatusFailed {
			add(attentionItem{Type: "unit", Severity: severityCritical, System: u.System, Name: u.Name, Reason: "Service failed"})
		}
	}

	updates := 0
	for _, c := range f.Containers {
		if !systemUp[c.System] {
			continue // records of a system that is down are stale
		}
		switch {
		case c.Health == 3: // unhealthy
			add(attentionItem{Type: "container", Severity: severityWarning, System: c.System, Name: c.Name, Reason: "Unhealthy"})
		case strings.HasPrefix(c.Status, "Restarting"):
			add(attentionItem{Type: "container", Severity: severityWarning, System: c.System, Name: c.Name, Reason: "Restarting"})
		case (strings.HasPrefix(c.Status, "Exited") && !strings.HasPrefix(c.Status, "Exited (0)")) || strings.HasPrefix(c.Status, "Dead"):
			add(attentionItem{Type: "container", Severity: severityWarning, System: c.System, Name: c.Name, Reason: c.Status})
		}
		if c.Updatable {
			updates++
			add(attentionItem{Type: "update", Severity: severityInfo, System: c.System, Name: c.Name, Reason: "Image update available"})
		}
	}

	rank := map[string]int{severityCritical: 0, severityWarning: 1, severityInfo: 2}
	sort.SliceStable(items, func(i, j int) bool {
		if ri, rj := rank[items[i].Severity], rank[items[j].Severity]; ri != rj {
			return ri < rj
		}
		if items[i].Type != items[j].Type {
			return items[i].Type < items[j].Type
		}
		return systemName[items[i].System]+items[i].Name < systemName[items[j].System]+items[j].Name
	})
	counts := map[string]int{severityCritical: 0, severityWarning: 0, severityInfo: 0}
	for _, item := range items {
		counts[item.Severity]++
	}

	overview := attentionOverview{
		SystemsTotal:  len(f.Systems),
		MonitorsUp:    monitorsUp,
		MonitorsTotal: monitorsTotal,
		UpdatesAvail:  updates,
		Hosts:         []attentionHost{},
		Pools:         []attentionPool{},
	}
	if monitorsUp > 0 {
		overview.AvgResponseUs = responseSum / float64(monitorsUp)
	}
	parents := map[string]bool{}
	for _, s := range f.Systems {
		if systemUp[s.ID] {
			overview.SystemsUp++
		}
		if s.Parent != "" {
			parents[s.Parent] = true
		}
	}
	for _, s := range f.Systems {
		if !parents[s.ID] {
			continue
		}
		var info struct {
			Cpu float64 `json:"cpu"`
			Mp  float64 `json:"mp"`
		}
		_ = json.Unmarshal([]byte(s.Info), &info)
		d := f.Details[s.ID]
		overview.Hosts = append(overview.Hosts, attentionHost{ID: s.ID, Name: s.Name, CpuPct: info.Cpu, MemPct: info.Mp, Cores: d.Cores, MemBytes: d.Memory})
	}
	for _, p := range f.Pools {
		pct := 0.0
		if p.Size > 0 {
			pct = p.Alloc * 100 / p.Size
		}
		overview.Pools = append(overview.Pools, attentionPool{System: p.System, Name: p.Name, Health: p.Health, UsedPct: pct, Size: p.Size})
	}
	sort.Slice(overview.Pools, func(i, j int) bool { return overview.Pools[i].Name < overview.Pools[j].Name })

	return attentionResponse{Items: items, Counts: counts, Overview: overview}
}

// collectAttentionFacts reads what the hub knows about the given systems.
func collectAttentionFacts(app core.App, systemIDs []string, now time.Time) (attentionFacts, error) {
	facts := attentionFacts{Now: now, Details: map[string]factDetails{}}
	if len(systemIDs) == 0 {
		return facts, nil
	}
	db := app.DB()
	run := func(target any, query string, extra dbx.Params) error {
		params := dbx.Params{}
		for k, v := range extra {
			params[k] = v
		}
		return db.NewQuery(strings.ReplaceAll(query, "%IDS%", inClause("s", systemIDs, params))).Bind(params).All(target)
	}
	if err := run(&facts.Systems, "SELECT id, name, status, updated, COALESCE(parent, '') AS parent, COALESCE(info, '') AS info FROM systems WHERE id IN (%IDS%)", nil); err != nil {
		return facts, err
	}
	if err := run(&facts.Alerts, "SELECT system, name, value, updated FROM alerts WHERE triggered = 1 AND system IN (%IDS%)", nil); err != nil {
		return facts, err
	}
	if err := run(&facts.Monitors, "SELECT system, COALESCE(name, '') AS name, target, protocol, port, res, updated FROM network_monitors WHERE enabled = 1 AND system IN (%IDS%)", nil); err != nil {
		return facts, err
	}
	if err := run(&facts.Pools, "SELECT system, name, health, alloc, size FROM zfs_pools WHERE system IN (%IDS%)", nil); err != nil {
		return facts, err
	}
	if err := run(&facts.Smart, "SELECT system, name, model, state FROM smart_devices WHERE system IN (%IDS%)", nil); err != nil {
		return facts, err
	}
	if err := run(&facts.Units, "SELECT system, name, state FROM systemd_services WHERE updated > {:fresh} AND system IN (%IDS%)", dbx.Params{"fresh": now.Add(-20 * time.Minute).UnixMilli()}); err != nil {
		return facts, err
	}
	if err := run(&facts.Containers, "SELECT system, name, status, health, updatable FROM containers WHERE updated > {:fresh} AND system IN (%IDS%)", dbx.Params{"fresh": now.Add(-10 * time.Minute).UnixMilli()}); err != nil {
		return facts, err
	}
	var details []struct {
		System string  `db:"system"`
		Cores  float64 `db:"cores"`
		Memory float64 `db:"memory"`
	}
	if err := run(&details, "SELECT system, cores, memory FROM system_details WHERE system IN (%IDS%)", nil); err != nil {
		return facts, err
	}
	for _, d := range details {
		facts.Details[d.System] = factDetails{Cores: d.Cores, Memory: d.Memory}
	}
	return facts, nil
}

// getAttention lists what needs attention across the systems the user can read, plus overview numbers.
// GET /api/beszel/fork/attention
func (h *Hub) getAttention(e *core.RequestEvent) error {
	systemIDs, err := accessibleSystemIDs(e)
	if err != nil {
		return e.InternalServerError("", err)
	}
	facts, err := collectAttentionFacts(e.App, systemIDs, time.Now())
	if err != nil {
		return e.InternalServerError("", err)
	}
	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, buildAttention(facts))
}
