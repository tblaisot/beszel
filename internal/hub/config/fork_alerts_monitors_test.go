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

const forkConfig = `systems:
  - name: nas
    host: 10.0.0.1
    users: [admin@example.com]
  - name: n8n
    host: 10.0.0.5
    users: [admin@example.com]
    parent: nas
    alerts:
      - { name: Disk, value: 80, min: 15 }
      - { name: Status }
      - { name: NoSuchAlert, value: 1 }
    monitors:
      - { name: Immich, protocol: http, target: "http://10.0.0.7:2283/", interval: 30 }
      - { name: DB, protocol: tcp, target: 10.0.0.8, port: 5432 }
`

func TestForkConfigExtras(t *testing.T) {
	testHub, err := tests.NewTestHub(t.TempDir())
	require.NoError(t, err)
	defer testHub.Cleanup()
	_, err = tests.CreateUser(testHub.App, "admin@example.com", "testtesttest")
	require.NoError(t, err)

	sync := func(yml string) {
		require.NoError(t, os.WriteFile(filepath.Join(testHub.DataDir(), "config.yml"), []byte(yml), 0644))
		require.NoError(t, config.SyncSystems(&core.ServeEvent{App: testHub.App}))
	}
	count := func(collection string) int {
		records, err := testHub.FindAllRecords(collection)
		require.NoError(t, err)
		return len(records)
	}

	sync(forkConfig)
	n8n, err := testHub.FindFirstRecordByFilter("systems", "name='n8n'")
	require.NoError(t, err)
	nas, err := testHub.FindFirstRecordByFilter("systems", "name='nas'")
	require.NoError(t, err)
	assert.Equal(t, nas.Id, n8n.GetString("parent"))
	assert.Equal(t, 2, count("alerts"), "unknown alert names are skipped")
	assert.Equal(t, 2, count("network_monitors"))
	disk, err := testHub.FindFirstRecordByFilter("alerts", "name='Disk'")
	require.NoError(t, err)
	assert.Equal(t, 80.0, disk.GetFloat("value"))
	assert.Equal(t, 15, disk.GetInt("min"))
	immich, err := testHub.FindFirstRecordByFilter("network_monitors", "name='Immich'")
	require.NoError(t, err)
	assert.Equal(t, 30, immich.GetInt("interval"))

	// a second run changes nothing
	sync(forkConfig)
	assert.Equal(t, 2, count("alerts"))
	assert.Equal(t, 2, count("network_monitors"))

	// declared fields are updated; undeclared things added by hand survive
	_, err = tests.CreateRecord(testHub.App, "alerts", map[string]any{"system": n8n.Id, "user": n8n.GetStringSlice("users")[0], "name": "CPU", "value": 90, "min": 10})
	require.NoError(t, err)
	changed := `systems:
  - name: nas
    host: 10.0.0.1
    users: [admin@example.com]
  - name: n8n
    host: 10.0.0.5
    users: [admin@example.com]
    alerts:
      - { name: Disk, value: 70, min: 5 }
    monitors:
      - { name: Immich, protocol: http, target: "http://10.0.0.7:2283/health", interval: 30 }
`
	sync(changed)
	disk, err = testHub.FindFirstRecordByFilter("alerts", "name='Disk'")
	require.NoError(t, err)
	assert.Equal(t, 70.0, disk.GetFloat("value"))
	assert.Equal(t, 3, count("alerts"), "Status and the hand-made CPU alert are kept without prune")
	assert.Equal(t, 2, count("network_monitors"), "DB is kept without prune; Immich was recreated")
	immich, err = testHub.FindFirstRecordByFilter("network_monitors", "name='Immich'")
	require.NoError(t, err)
	assert.Equal(t, "http://10.0.0.7:2283/health", immich.GetString("target"))

	// prune removes what is not declared
	sync(changed + "    prune: true\n")
	assert.Equal(t, 1, count("alerts"))
	assert.Equal(t, 1, count("network_monitors"))
}
