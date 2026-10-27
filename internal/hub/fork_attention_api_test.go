//go:build testing

package hub_test

import (
	"net/http"
	"testing"

	beszelTests "github.com/henrygd/beszel/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

func TestAttentionEndpoint(t *testing.T) {
	hub, user := beszelTests.GetHubWithUser(t)
	defer hub.Cleanup()
	userToken, err := user.NewAuthToken()
	require.NoError(t, err)
	other, err := beszelTests.CreateUser(hub, "other@example.com", "password123")
	require.NoError(t, err)
	otherToken, err := other.NewAuthToken()
	require.NoError(t, err)

	mine, err := beszelTests.CreateRecord(hub, "systems", map[string]any{
		"name": "mine", "host": "10.0.0.1", "port": "45876", "status": "up", "users": []string{user.Id},
	})
	require.NoError(t, err)
	_, err = beszelTests.CreateRecord(hub, "systems", map[string]any{
		"name": "theirs", "host": "10.0.0.2", "port": "45876", "status": "up", "users": []string{other.Id},
	})
	require.NoError(t, err)
	_, err = beszelTests.CreateRecord(hub, "smart_devices", map[string]any{
		"system": mine.Id, "name": "sda", "model": "M", "state": "FAILED",
	})
	require.NoError(t, err)

	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }
	scenarios := []beszelTests.ApiScenario{
		{
			Name: "no auth", Method: http.MethodGet, URL: "/api/beszel/fork/attention",
			ExpectedStatus: 401, ExpectedContent: []string{"requires valid"}, TestAppFactory: testAppFactory,
		},
		{
			Name: "own systems only", Method: http.MethodGet, URL: "/api/beszel/fork/attention",
			Headers: map[string]string{"Authorization": userToken}, ExpectedStatus: 200,
			ExpectedContent:    []string{`"name":"sda (M)"`, `"type":"smart"`, `"critical":1`, `"systemsTotal":1`},
			NotExpectedContent: []string{"theirs"}, TestAppFactory: testAppFactory,
		},
		{
			Name: "other user", Method: http.MethodGet, URL: "/api/beszel/fork/attention",
			Headers: map[string]string{"Authorization": otherToken}, ExpectedStatus: 200,
			ExpectedContent:    []string{`"items":[]`, `"critical":0`, `"systemsTotal":1`},
			NotExpectedContent: []string{"smart", "sda"}, TestAppFactory: testAppFactory,
		},
	}
	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
