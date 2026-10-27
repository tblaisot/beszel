//go:build testing

package hub

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The monitor name is not part of the monitor ID, so it must survive the
// copy that happens when an edit changes the monitor's identity.
func TestCopyMonitorToNewRecordKeepsName(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	collection, err := hub.FindCachedCollectionByNameOrId("network_monitors")
	require.NoError(t, err)
	require.NotNil(t, collection.Fields.GetByName("name"))

	oldRecord := core.NewRecord(collection)
	oldRecord.Load(map[string]any{
		"system": "sys123", "name": "Immich", "target": "http://10.0.0.7:2283/",
		"protocol": "http", "interval": 60, "enabled": true,
	})

	newRecord := copyMonitorToNewRecord(oldRecord, "next12345")
	assert.Equal(t, "Immich", newRecord.GetString("name"))
	assert.Equal(t, "", copyMonitorToNewRecord(core.NewRecord(collection), "x").GetString("name"))
}
