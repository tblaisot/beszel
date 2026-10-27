//go:build testing

package hub

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A system can point to the host it runs on; the link is optional and self-referencing.
func TestSystemParentField(t *testing.T) {
	hub, testApp, err := createTestHub(t)
	require.NoError(t, err)
	defer cleanupTestHub(hub, testApp)

	collection, err := hub.FindCachedCollectionByNameOrId("systems")
	require.NoError(t, err)
	field, ok := collection.Fields.GetByName("parent").(*core.RelationField)
	require.True(t, ok, "systems.parent must be a relation field")
	assert.Equal(t, collection.Id, field.CollectionId)
	assert.Equal(t, 1, field.MaxSelect)

	user, err := createTestUser(hub)
	require.NoError(t, err)
	host, err := createTestRecord(hub, "systems", map[string]any{
		"name": "host", "host": "10.0.0.1", "port": "45876", "status": "up", "users": []string{user.Id},
	})
	require.NoError(t, err)
	guest, err := createTestRecord(hub, "systems", map[string]any{
		"name": "guest", "host": "10.0.0.2", "port": "45876", "status": "up", "users": []string{user.Id}, "parent": host.Id,
	})
	require.NoError(t, err)
	assert.Equal(t, host.Id, guest.GetString("parent"))
	assert.Equal(t, "", host.GetString("parent"))
}
