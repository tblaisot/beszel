//go:build testing

package hub

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBuildAttention(t *testing.T) {
	facts := attentionFacts{
		Now: time.Now(),
		Systems: []factSystem{
			{ID: "nas", Name: "nas", Status: "up", Info: `{"cpu":10,"mp":50}`},
			{ID: "app", Name: "app", Status: "up", Parent: "nas"},
			{ID: "gone", Name: "gone", Status: "down", Updated: "2026-10-27 07:00:00.000Z", Parent: "nas"},
			{ID: "idle", Name: "idle", Status: "paused"},
		},
		Details: map[string]factDetails{"nas": {Cores: 8, Memory: 32 << 30}},
		Alerts: []factAlert{
			{System: "app", Name: "Disk"},
			{System: "gone", Name: "Status"}, // already covered by the down system
		},
		Monitors: []factMonitor{
			{System: "app", Name: "Web", Target: "http://a/", Protocol: "http", Res: 1000, Updated: "x"},
			{System: "app", Target: "10.0.0.2", Protocol: "tcp", Port: 5432, Res: 0, Updated: "x"},
			{System: "app", Target: "never-ran", Protocol: "icmp", Res: 0, Updated: ""},
			{System: "idle", Target: "paused-system", Protocol: "icmp", Res: 0, Updated: "x"},
		},
		Pools: []factPool{
			{System: "nas", Name: "tank", Health: "DEGRADED", Alloc: 25, Size: 100},
			{System: "nas", Name: "fast", Health: "ONLINE", Alloc: 50, Size: 100},
			{System: "nas", Name: "dead", Health: "FAULTED", Alloc: 1, Size: 100},
		},
		Smart: []factSmart{
			{System: "nas", Name: "sda", Model: "IronWolf", State: "FAILED"},
			{System: "nas", Name: "sdb", State: "PASSED"},
			{System: "nas", Name: "sdc", State: "UNKNOWN"},
		},
		Units: []factUnit{{System: "app", Name: "stack-web.service", State: 2}, {System: "app", Name: "ok.service", State: 0}},
		Containers: []factContainer{
			{System: "app", Name: "db", Health: 3},
			{System: "app", Name: "loop", Status: "Restarting (1) 2 seconds ago"},
			{System: "app", Name: "crashed", Status: "Exited (137) 3 minutes ago"},
			{System: "app", Name: "job", Status: "Exited (0) 3 minutes ago"},
			{System: "app", Name: "old", Status: "Up 2 days", Updatable: true},
			{System: "gone", Name: "stale", Health: 3}, // system is down, record is stale
		},
	}

	got := buildAttention(facts)

	byType := map[string][]attentionItem{}
	for _, item := range got.Items {
		byType[item.Type] = append(byType[item.Type], item)
	}
	assert.Len(t, byType["system"], 1)
	assert.Equal(t, "gone", byType["system"][0].Name)
	assert.Len(t, byType["alert"], 1, "the Status alert is not repeated")
	assert.Len(t, byType["monitor"], 1, "unprobed and paused-system monitors are ignored")
	assert.Equal(t, "10.0.0.2:5432", byType["monitor"][0].Name)
	assert.Len(t, byType["zfs"], 2)
	assert.Len(t, byType["smart"], 1)
	assert.Equal(t, "sda (IronWolf)", byType["smart"][0].Name)
	assert.Len(t, byType["unit"], 1)
	assert.Len(t, byType["container"], 3, "unhealthy, restarting and crashed; a clean exit and a down system are ignored")
	assert.Len(t, byType["update"], 1)

	// critical first, info last
	assert.Equal(t, severityCritical, got.Items[0].Severity)
	assert.Equal(t, severityInfo, got.Items[len(got.Items)-1].Severity)
	assert.Equal(t, map[string]int{severityCritical: 5, severityWarning: 5, severityInfo: 1}, got.Counts)

	o := got.Overview
	assert.Equal(t, 4, o.SystemsTotal)
	assert.Equal(t, 2, o.SystemsUp)
	assert.Equal(t, 2, o.MonitorsTotal)
	assert.Equal(t, 1, o.MonitorsUp)
	assert.InDelta(t, 1000, o.AvgResponseUs, 0.001)
	assert.Equal(t, 1, o.UpdatesAvail)
	if assert.Len(t, o.Hosts, 1) {
		assert.Equal(t, "nas", o.Hosts[0].Name)
		assert.Equal(t, 8.0, o.Hosts[0].Cores)
		assert.Equal(t, 50.0, o.Hosts[0].MemPct)
	}
	if assert.Len(t, o.Pools, 3) {
		assert.Equal(t, "dead", o.Pools[0].Name)
		assert.Equal(t, 25.0, o.Pools[2].UsedPct)
	}
}

func TestBuildAttentionAllWell(t *testing.T) {
	got := buildAttention(attentionFacts{Systems: []factSystem{{ID: "a", Name: "a", Status: "up"}}})
	assert.Empty(t, got.Items)
	assert.Equal(t, map[string]int{severityCritical: 0, severityWarning: 0, severityInfo: 0}, got.Counts)
	assert.Equal(t, 1, got.Overview.SystemsUp)
}
