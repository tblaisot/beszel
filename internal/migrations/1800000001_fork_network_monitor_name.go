package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Fork migration: optional display name for a network monitor.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("network_monitors")
		if err != nil {
			return err
		}
		collection.Fields.Add(&core.TextField{Id: "fork_nm_name", Name: "name", Max: 100})
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("network_monitors")
		if err != nil {
			return err
		}
		collection.Fields.RemoveByName("name")
		return app.Save(collection)
	})
}
