package config

import (
	"database/sql"
	"errors"
	"log"
	"slices"
	"strconv"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// forkSystemConfig holds the fork's optional keys of a system entry in config.yml.
type forkSystemConfig struct {
	Parent   string          `yaml:"parent,omitempty"`
	Prune    bool            `yaml:"prune,omitempty"`
	Alerts   []alertConfig   `yaml:"alerts,omitempty"`
	Monitors []monitorConfig `yaml:"monitors,omitempty"`
}

type alertConfig struct {
	Name  string  `yaml:"name"`
	Value float64 `yaml:"value,omitempty"`
	Min   uint8   `yaml:"min,omitempty"`
}

type monitorConfig struct {
	Name          string `yaml:"name"`
	Protocol      string `yaml:"protocol"`
	Target        string `yaml:"target"`
	Port          uint16 `yaml:"port,omitempty"`
	Server        string `yaml:"server,omitempty"`
	Interval      uint16 `yaml:"interval,omitempty"`
	Disabled      bool   `yaml:"disabled,omitempty"`
	SkipTLSVerify bool   `yaml:"skipTlsVerify,omitempty"`
}

const defaultMonitorInterval = 60

// syncForkExtras applies the parent link, alerts and network monitors declared under
// each system. It creates what is missing and updates what is declared; it only
// deletes undeclared alerts and monitors of a system that sets prune. Problems are
// logged and skipped so a bad entry never stops the hub from starting.
func syncForkExtras(app core.App, configs []systemConfig) {
	for _, sc := range configs {
		if sc.Parent == "" && len(sc.Alerts) == 0 && len(sc.Monitors) == 0 && !sc.Prune {
			continue
		}
		system, err := app.FindFirstRecordByFilter("systems", "name={:name} && host={:host} && port={:port}",
			dbx.Params{"name": sc.Name, "host": sc.Host, "port": strconv.Itoa(int(sc.Port))})
		if err != nil {
			log.Printf("config.yml: system %s not found: %v", sc.Name, err)
			continue
		}
		syncParent(app, system, sc.Parent)
		syncAlerts(app, system, sc)
		syncMonitors(app, system, sc)
	}
}

func syncParent(app core.App, system *core.Record, parentName string) {
	if parentName == "" {
		return
	}
	parent, err := app.FindFirstRecordByFilter("systems", "name={:name}", dbx.Params{"name": parentName})
	if err != nil || parent.Id == system.Id {
		log.Printf("config.yml: parent %q of system %s is not usable", parentName, system.GetString("name"))
		return
	}
	if system.GetString("parent") == parent.Id {
		return
	}
	system.Set("parent", parent.Id)
	if err := app.Save(system); err != nil {
		log.Printf("config.yml: failed to set parent of %s: %v", system.GetString("name"), err)
	}
}

// syncAlerts keeps one alert record per declared alert and user of the system.
func syncAlerts(app core.App, system *core.Record, sc systemConfig) {
	if len(sc.Alerts) == 0 && !sc.Prune {
		return
	}
	collection, err := app.FindCachedCollectionByNameOrId("alerts")
	if err != nil {
		log.Printf("config.yml: %v", err)
		return
	}
	known := collection.Fields.GetByName("name").(*core.SelectField).Values
	declared := make(map[string]bool, len(sc.Alerts))
	for _, a := range sc.Alerts {
		if !slices.Contains(known, a.Name) {
			log.Printf("config.yml: unknown alert %q on system %s, skipped", a.Name, sc.Name)
			continue
		}
		declared[a.Name] = true
		for _, userID := range system.GetStringSlice("users") {
			record, err := app.FindFirstRecordByFilter(collection, "system={:system} && name={:name} && user={:user}",
				dbx.Params{"system": system.Id, "name": a.Name, "user": userID})
			if err != nil {
				if !errors.Is(err, sql.ErrNoRows) {
					log.Printf("config.yml: %v", err)
					continue
				}
				record = core.NewRecord(collection)
				record.Set("system", system.Id)
				record.Set("name", a.Name)
				record.Set("user", userID)
			} else if record.GetFloat("value") == a.Value && record.GetInt("min") == int(a.Min) {
				continue
			}
			record.Set("value", a.Value)
			record.Set("min", a.Min)
			if err := app.SaveNoValidate(record); err != nil {
				log.Printf("config.yml: failed to save alert %s on %s: %v", a.Name, sc.Name, err)
			}
		}
	}
	if !sc.Prune {
		return
	}
	existing, err := app.FindAllRecords(collection, dbx.HashExp{"system": system.Id})
	if err != nil {
		return
	}
	for _, record := range existing {
		if !declared[record.GetString("name")] {
			if err := app.Delete(record); err != nil {
				log.Printf("config.yml: failed to prune alert: %v", err)
			}
		}
	}
}

// syncMonitors matches monitors of the system by name. Changing what a monitor probes
// changes its id upstream, so it is recreated instead of updated.
func syncMonitors(app core.App, system *core.Record, sc systemConfig) {
	if len(sc.Monitors) == 0 && !sc.Prune {
		return
	}
	collection, err := app.FindCachedCollectionByNameOrId("network_monitors")
	if err != nil {
		log.Printf("config.yml: %v", err)
		return
	}
	declared := make(map[string]bool, len(sc.Monitors))
	for _, m := range sc.Monitors {
		if m.Name == "" {
			log.Printf("config.yml: a monitor of system %s has no name, skipped", sc.Name)
			continue
		}
		declared[m.Name] = true
		interval := m.Interval
		if interval == 0 {
			interval = defaultMonitorInterval
		}
		apply := func(r *core.Record) {
			r.Set("system", system.Id)
			r.Set("name", m.Name)
			r.Set("protocol", m.Protocol)
			r.Set("target", m.Target)
			r.Set("port", m.Port)
			r.Set("server", m.Server)
			r.Set("interval", interval)
			r.Set("enabled", !m.Disabled)
			r.Set("skipTlsVerify", m.SkipTLSVerify)
		}
		record, err := app.FindFirstRecordByFilter(collection, "system={:system} && name={:name}",
			dbx.Params{"system": system.Id, "name": m.Name})
		if err == nil {
			sameProbe := record.GetString("protocol") == m.Protocol && record.GetString("target") == m.Target &&
				record.GetInt("port") == int(m.Port) && record.GetString("server") == m.Server
			if !sameProbe {
				if err := app.Delete(record); err != nil {
					log.Printf("config.yml: failed to replace monitor %s: %v", m.Name, err)
					continue
				}
			} else {
				apply(record)
				if err := app.Save(record); err != nil {
					log.Printf("config.yml: failed to update monitor %s: %v", m.Name, err)
				}
				continue
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("config.yml: %v", err)
			continue
		}
		record = core.NewRecord(collection)
		apply(record)
		if err := app.Save(record); err != nil {
			log.Printf("config.yml: failed to create monitor %s on %s: %v", m.Name, sc.Name, err)
		}
	}
	if !sc.Prune {
		return
	}
	existing, err := app.FindAllRecords(collection, dbx.HashExp{"system": system.Id})
	if err != nil {
		return
	}
	for _, record := range existing {
		if !declared[record.GetString("name")] {
			if err := app.Delete(record); err != nil {
				log.Printf("config.yml: failed to prune monitor: %v", err)
			}
		}
	}
}
