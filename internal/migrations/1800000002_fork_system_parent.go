package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Fork migration: optional parent system (the physical host a VM, LXC or container host runs on).
// It is a plain manual link used to show CPU and memory as a share of the host.
func init() {
	m.Register(func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		collection.Fields.Add(&core.RelationField{
			Id:            "fork_sys_parent",
			Name:          "parent",
			CollectionId:  collection.Id,
			MaxSelect:     1,
			CascadeDelete: false,
		})
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("systems")
		if err != nil {
			return err
		}
		collection.Fields.RemoveByName("parent")
		return app.Save(collection)
	})
}
