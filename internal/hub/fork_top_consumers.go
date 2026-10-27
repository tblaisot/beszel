package hub

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/henrygd/beszel/internal/entities/container"
	"github.com/henrygd/beszel/internal/hub/utils"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// topConsumerWindows maps a requested window to the stats tier that covers it.
// The tiers and their retention are the ones records.DeleteOldRecords keeps.
var topConsumerWindows = map[string]struct {
	recordType string
	duration   time.Duration
}{
	"1h":  {"1m", time.Hour},
	"12h": {"10m", 12 * time.Hour},
	"24h": {"20m", 24 * time.Hour},
	"1w":  {"120m", 7 * 24 * time.Hour},
	"30d": {"480m", 30 * 24 * time.Hour},
}

const (
	topConsumersDefaultLimit = 10
	topConsumersMaxLimit     = 50
)

// topConsumer is a container's average usage over the requested window.
type topConsumer struct {
	System string  `json:"s"`
	Name   string  `json:"n"`
	Cpu    float64 `json:"c"` // average cpu, percent of the system
	Mem    float64 `json:"m"` // average memory, MiB
}

type topConsumerRow struct {
	System string `db:"system"`
	Stats  string `db:"stats"`
}

// aggregateTopConsumers averages each container over the records it appears in and ranks the
// result by absolute usage (cores for cpu, MiB for memory), so containers of differently sized
// systems are comparable. cores holds each system's core count; unknown systems count as one core.
func aggregateTopConsumers(rows []topConsumerRow, cores map[string]float64, metric string, limit int) []topConsumer {
	type key struct{ system, name string }
	type sum struct {
		cpu, mem float64
		n        int
	}
	sums := map[key]*sum{}
	for _, row := range rows {
		var stats []container.Stats
		if err := json.Unmarshal([]byte(row.Stats), &stats); err != nil {
			continue
		}
		for _, s := range stats {
			k := key{row.System, s.Name}
			agg := sums[k]
			if agg == nil {
				agg = &sum{}
				sums[k] = agg
			}
			agg.cpu += s.Cpu
			agg.mem += s.Mem
			agg.n++
		}
	}

	items := make([]topConsumer, 0, len(sums))
	for k, agg := range sums {
		items = append(items, topConsumer{
			System: k.system,
			Name:   k.name,
			Cpu:    agg.cpu / float64(agg.n),
			Mem:    agg.mem / float64(agg.n),
		})
	}
	rank := func(c topConsumer) float64 {
		if metric == "memory" {
			return c.Mem
		}
		n := cores[c.System]
		if n <= 0 {
			n = 1
		}
		return c.Cpu * n
	}
	sort.Slice(items, func(i, j int) bool {
		if ri, rj := rank(items[i]), rank(items[j]); ri != rj {
			return ri > rj
		}
		return items[i].Name < items[j].Name
	})
	if len(items) > limit {
		items = items[:limit]
	}
	return items
}

// accessibleSystemIDs returns the systems the requesting user may read, mirroring the
// systems list rule (members only, or everyone when SHARE_ALL_SYSTEMS is set).
func accessibleSystemIDs(e *core.RequestEvent) ([]string, error) {
	shareAll, _ := utils.GetEnv("SHARE_ALL_SYSTEMS")
	var records []*core.Record
	var err error
	if shareAll == "true" || e.Auth.Collection().Name == core.CollectionNameSuperusers {
		records, err = e.App.FindAllRecords("systems")
	} else {
		records, err = e.App.FindRecordsByFilter("systems", "users.id ?= {:uid}", "", 0, 0, dbx.Params{"uid": e.Auth.Id})
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(records))
	for i, record := range records {
		ids[i] = record.Id
	}
	return ids, nil
}

// getTopConsumers ranks containers across all readable systems by their average usage over a window.
// GET /api/beszel/fork/top-consumers?metric=cpu|memory&window=1h|12h|24h|1w|30d&limit=10
func (h *Hub) getTopConsumers(e *core.RequestEvent) error {
	query := e.Request.URL.Query()
	metric := query.Get("metric")
	if metric == "" {
		metric = "cpu"
	}
	if metric != "cpu" && metric != "memory" {
		return e.BadRequestError("metric must be cpu or memory", nil)
	}
	window := query.Get("window")
	if window == "" {
		window = "1h"
	}
	spec, ok := topConsumerWindows[window]
	if !ok {
		return e.BadRequestError("invalid window", nil)
	}
	limit := topConsumersDefaultLimit
	if raw := query.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > topConsumersMaxLimit {
			return e.BadRequestError("limit must be between 1 and "+strconv.Itoa(topConsumersMaxLimit), nil)
		}
		limit = n
	}

	systemIDs, err := accessibleSystemIDs(e)
	if err != nil {
		return e.InternalServerError("", err)
	}
	items := []topConsumer{}
	if len(systemIDs) > 0 {
		params := dbx.Params{
			"type": spec.recordType,
			"from": time.Now().UTC().Add(-spec.duration).Format(types.DefaultDateLayout),
		}
		placeholders := make([]string, len(systemIDs))
		for i, id := range systemIDs {
			name := "s" + strconv.Itoa(i)
			placeholders[i] = "{:" + name + "}"
			params[name] = id
		}
		var rows []topConsumerRow
		err = e.App.DB().NewQuery(
			"SELECT system, stats FROM container_stats WHERE type = {:type} AND created > {:from} AND system IN (" +
				strings.Join(placeholders, ",") + ")",
		).Bind(params).All(&rows)
		if err != nil {
			return e.InternalServerError("", err)
		}

		var details []struct {
			System string  `db:"system"`
			Cores  float64 `db:"cores"`
		}
		if err = e.App.DB().NewQuery("SELECT system, cores FROM system_details").All(&details); err != nil {
			return e.InternalServerError("", err)
		}
		cores := make(map[string]float64, len(details))
		for _, d := range details {
			cores[d.System] = d.Cores
		}
		items = aggregateTopConsumers(rows, cores, metric, limit)
	}

	e.Response.Header().Set("Cache-Control", "no-store")
	return e.JSON(http.StatusOK, map[string]any{"window": window, "metric": metric, "items": items})
}
