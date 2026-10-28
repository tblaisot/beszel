//go:build testing

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/henrygd/beszel/internal/hub/config"
	"github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForkConfigUsers(t *testing.T) {
	testHub, err := tests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer testHub.Cleanup()
	existing, err := tests.CreateUser(testHub.App, "existing@example.com", "oldpassword1")
	require.NoError(t, err)

	t.Setenv("HOMEPAGE_PW", "homepage-secret-1")
	t.Setenv("ADMIN_PW", "new-admin-pw-1")
	t.Setenv("NOTIFY_URL", "generic://mailrise.lan/token123")
	t.Setenv("EMPTY_VAR", "")

	const yml = `users:
  - email: ops@example.com
    role: admin
    password_env: ADMIN_PW
    notifications:
      webhooks: ["${NOTIFY_URL}", "${EMPTY_VAR}", "${UNSET_VAR}"]
    settings: { unitTemp: fahrenheit, chartTime: 12h }
  - email: homepage@example.com
    role: readonly
    password_env: HOMEPAGE_PW
  - email: existing@example.com
    role: admin
    notifications:
      emails: [alerts@example.com]
  - email: nopassword@example.com
  - email: badrole@example.com
    role: root
    password_env: HOMEPAGE_PW
`
	sync := func(content string) {
		require.NoError(t, os.WriteFile(filepath.Join(testHub.DataDir(), "config.yml"), []byte(content), 0644))
		require.NoError(t, config.SyncSystems(&core.ServeEvent{App: testHub.App}), "a users-only config is applied")
	}
	settingsOf := func(user *core.Record) map[string]any {
		record, err := testHub.FindFirstRecordByFilter("user_settings", "user={:u}", map[string]any{"u": user.Id})
		require.NoError(t, err)
		var s map[string]any
		require.NoError(t, record.UnmarshalJSONField("settings", &s))
		return s
	}

	sync(yml)

	ops, err := testHub.FindAuthRecordByEmail("users", "ops@example.com")
	require.NoError(t, err)
	assert.Equal(t, "admin", ops.GetString("role"))
	assert.True(t, ops.ValidatePassword("new-admin-pw-1"))
	s := settingsOf(ops)
	assert.Equal(t, []any{"generic://mailrise.lan/token123"}, s["webhooks"], "env references are expanded, unset ones dropped")
	assert.Equal(t, "fahrenheit", s["unitTemp"])
	assert.Equal(t, "12h", s["chartTime"])

	homepage, err := testHub.FindAuthRecordByEmail("users", "homepage@example.com")
	require.NoError(t, err)
	assert.Equal(t, "readonly", homepage.GetString("role"))

	_, err = testHub.FindAuthRecordByEmail("users", "nopassword@example.com")
	assert.Error(t, err, "no password, no user")
	_, err = testHub.FindAuthRecordByEmail("users", "badrole@example.com")
	assert.Error(t, err, "unknown role is refused")

	existing, err = testHub.FindAuthRecordByEmail("users", "existing@example.com")
	require.NoError(t, err)
	assert.Equal(t, "admin", existing.GetString("role"))
	assert.True(t, existing.ValidatePassword("oldpassword1"), "no password_env leaves the password alone")
	assert.Equal(t, []any{"alerts@example.com"}, settingsOf(existing)["emails"])

	// a second run changes nothing, including the session tokens
	tokenKey := ops.TokenKey()
	sync(yml)
	ops, err = testHub.FindAuthRecordByEmail("users", "ops@example.com")
	require.NoError(t, err)
	assert.Equal(t, tokenKey, ops.TokenKey(), "an unchanged password is not set again")
	users, err := testHub.FindAllRecords("users")
	require.NoError(t, err)
	assert.Len(t, users, 3)

	// a changed environment value is applied, other settings keep their values
	t.Setenv("ADMIN_PW", "rotated-admin-pw-2")
	t.Setenv("NOTIFY_URL", "generic://mailrise.lan/other")
	sync(yml)
	ops, err = testHub.FindAuthRecordByEmail("users", "ops@example.com")
	require.NoError(t, err)
	assert.True(t, ops.ValidatePassword("rotated-admin-pw-2"))
	assert.Equal(t, []any{"generic://mailrise.lan/other"}, settingsOf(ops)["webhooks"])
}
