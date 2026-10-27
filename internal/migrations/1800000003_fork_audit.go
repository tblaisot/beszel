package migrations

import (
	"github.com/pocketbase/pocketbase/core"
	m "github.com/pocketbase/pocketbase/migrations"
)

// Fork migration: append-only log of actions taken on systems (start, stop, restart, ...).
// Only the hub writes it; admins may read it.
func init() {
	m.Register(func(app core.App) error {
		collection := core.NewBaseCollection("fork_audit")
		adminRule := `@request.auth.id != "" && @request.auth.role = "admin"`
		collection.ListRule = &adminRule
		collection.ViewRule = &adminRule
		collection.Fields.Add(
			&core.TextField{Id: "fork_audit_user", Name: "user", Max: 50},
			&core.TextField{Id: "fork_audit_user_email", Name: "user_email", Max: 255},
			&core.TextField{Id: "fork_audit_system", Name: "system", Max: 50},
			&core.TextField{Id: "fork_audit_system_name", Name: "system_name", Max: 255},
			&core.TextField{Id: "fork_audit_kind", Name: "kind", Max: 50},
			&core.TextField{Id: "fork_audit_target", Name: "target", Max: 255},
			&core.TextField{Id: "fork_audit_action", Name: "action", Max: 50},
			&core.TextField{Id: "fork_audit_result", Name: "result", Max: 50},
			&core.TextField{Id: "fork_audit_message", Name: "message", Max: 1000},
			&core.AutodateField{Id: "fork_audit_created", Name: "created", OnCreate: true},
		)
		collection.AddIndex("idx_fork_audit_created", false, "created", "")
		return app.Save(collection)
	}, func(app core.App) error {
		collection, err := app.FindCollectionByNameOrId("fork_audit")
		if err != nil {
			return nil
		}
		return app.Delete(collection)
	})
}
