package config

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// forkConfig holds the fork's top-level keys of config.yml.
type forkConfig struct {
	Users []userConfig `yaml:"users,omitempty"`
}

// userConfig declares a Beszel user, its role and its notification settings. Secrets are never
// written in the file: the password comes from the environment variable named by PasswordEnv, and
// ${VAR} references in notification targets are replaced with the environment's value.
type userConfig struct {
	Email         string               `yaml:"email"`
	Role          string               `yaml:"role,omitempty"`
	PasswordEnv   string               `yaml:"password_env,omitempty"`
	Notifications *notificationsConfig `yaml:"notifications,omitempty"`
	// Settings are merged into the user's settings as they are (unitTemp, chartTime, lang...).
	Settings map[string]any `yaml:"settings,omitempty"`
}

type notificationsConfig struct {
	Emails   []string `yaml:"emails,omitempty"`
	Webhooks []string `yaml:"webhooks,omitempty"`
}

var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// expandEnv replaces ${VAR} by the value of the environment variable. It reports false when a
// referenced variable is unset or empty, so a half-built secret is never stored.
func expandEnv(value string) (string, bool) {
	complete := true
	out := envReference.ReplaceAllStringFunc(value, func(ref string) string {
		v := os.Getenv(envReference.FindStringSubmatch(ref)[1])
		if v == "" {
			complete = false
		}
		return v
	})
	return out, complete
}

// syncForkUsers creates the declared users that are missing, sets their role and password when they
// differ, and merges their notification targets and settings. It never deletes users. Problems are
// logged without the values involved (they may be secrets) and skipped.
func syncForkUsers(app core.App, users []userConfig) {
	for _, uc := range users {
		if uc.Email == "" {
			log.Print("config.yml: a user has no email, skipped")
			continue
		}
		if uc.Role != "" && !slices.Contains([]string{"user", "admin", "readonly"}, uc.Role) {
			log.Printf("config.yml: user %s has an unknown role, skipped", uc.Email)
			continue
		}
		user, err := ensureUser(app, uc)
		if err != nil {
			log.Printf("config.yml: user %s: %v", uc.Email, err)
			continue
		}
		if err := syncUserSettings(app, user, uc); err != nil {
			log.Printf("config.yml: settings of %s: %v", uc.Email, err)
		}
	}
}

func ensureUser(app core.App, uc userConfig) (*core.Record, error) {
	password := ""
	if uc.PasswordEnv != "" {
		if password = os.Getenv(uc.PasswordEnv); password == "" {
			log.Printf("config.yml: variable %s for user %s is not set", uc.PasswordEnv, uc.Email)
		}
	}
	user, err := app.FindAuthRecordByEmail("users", uc.Email)
	if errors.Is(err, sql.ErrNoRows) {
		if password == "" {
			return nil, errors.New("does not exist and has no password_env to create it with")
		}
		collection, err := app.FindCachedCollectionByNameOrId("users")
		if err != nil {
			return nil, err
		}
		user = core.NewRecord(collection)
		user.SetEmail(uc.Email)
		user.SetPassword(password)
		user.SetVerified(true)
		user.Set("role", cmpOr(uc.Role, "user"))
		return user, app.Save(user)
	}
	if err != nil {
		return nil, err
	}
	changed := false
	if uc.Role != "" && user.GetString("role") != uc.Role {
		user.Set("role", uc.Role)
		changed = true
	}
	// setting the password rotates the user's tokens, so only do it when it differs
	if password != "" && !user.ValidatePassword(password) {
		user.SetPassword(password)
		changed = true
	}
	if changed {
		return user, app.Save(user)
	}
	return user, nil
}

func cmpOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// syncUserSettings merges the declared keys into the user's settings record, creating it if needed.
func syncUserSettings(app core.App, user *core.Record, uc userConfig) error {
	if uc.Notifications == nil && len(uc.Settings) == 0 {
		return nil
	}
	settings := map[string]any{}
	record, err := app.FindFirstRecordByFilter("user_settings", "user={:user}", dbx.Params{"user": user.Id})
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		collection, err := app.FindCachedCollectionByNameOrId("user_settings")
		if err != nil {
			return err
		}
		record = core.NewRecord(collection)
		record.Set("user", user.Id)
		// Beszel's create hook fills in the defaults; the declared values are applied after it
		if err := app.Save(record); err != nil {
			return err
		}
	}
	if err := record.UnmarshalJSONField("settings", &settings); err != nil {
		settings = map[string]any{}
	}
	before, _ := json.Marshal(settings)
	for k, v := range uc.Settings {
		settings[k] = v
	}
	if n := uc.Notifications; n != nil {
		if n.Emails != nil {
			settings["emails"] = expandAll(n.Emails, uc.Email, "emails")
		}
		if n.Webhooks != nil {
			settings["webhooks"] = expandAll(n.Webhooks, uc.Email, "webhooks")
		}
	}
	if after, _ := json.Marshal(settings); string(after) == string(before) {
		return nil
	}
	record.Set("settings", settings)
	return app.Save(record)
}

// expandAll expands environment references in every entry; entries that cannot be completed are dropped.
func expandAll(values []string, email, kind string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		expanded, ok := expandEnv(strings.TrimSpace(v))
		if !ok {
			log.Printf("config.yml: a %s entry of %s refers to an unset variable, skipped", kind, email)
			continue
		}
		out = append(out, expanded)
	}
	return out
}
