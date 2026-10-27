//go:build testing

package hub_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub"
	"github.com/henrygd/beszel/internal/hub/systems"
	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/dbx"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlEndpoint(t *testing.T) {
	testHub, user := beszelTests.GetHubWithUser(t) // a regular user
	defer testHub.Cleanup()
	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	admin, err := beszelTests.CreateUserWithRole(testHub, "admin@example.com", "password123", "admin")
	require.NoError(t, err)
	adminToken, err := admin.NewAuthToken()
	require.NoError(t, err)
	outsider, err := beszelTests.CreateUserWithRole(testHub, "outsider@example.com", "password123", "admin")
	require.NoError(t, err)
	outsiderToken, err := outsider.NewAuthToken()
	require.NoError(t, err)

	allowed, err := beszelTests.CreateRecord(testHub, "systems", map[string]any{
		"name": "allowed", "host": "10.0.0.1", "port": "45876", "users": []string{admin.Id, user.Id},
	})
	require.NoError(t, err)
	// the hub rewrites info when a system is created, so set what the agent would report afterwards
	_, err = testHub.DB().NewQuery("UPDATE systems SET info = {:info} WHERE id = {:id}").
		Bind(dbx.Params{"info": `{"cpu":1,"fa":["control"]}`, "id": allowed.Id}).Execute()
	require.NoError(t, err)
	disabled, err := beszelTests.CreateRecord(testHub, "systems", map[string]any{
		"name": "disabled", "host": "10.0.0.2", "port": "45876", "users": []string{admin.Id},
	})
	require.NoError(t, err)

	var sent []common.ControlRequest
	var agentErr error
	restore := hub.SetForkControlAgent(func(_ *systems.System, req common.ControlRequest) (string, error) {
		sent = append(sent, req)
		if agentErr != nil {
			return "", agentErr
		}
		return "ok", nil
	})
	defer restore()

	auditCount := func(t testing.TB, app *pbTests.TestApp, result string) int {
		records, err := app.FindAllRecords("fork_audit")
		require.NoError(t, err)
		if result == "" {
			return len(records)
		}
		count := 0
		for _, r := range records {
			if r.GetString("result") == result {
				count++
			}
		}
		return count
	}
	testAppFactory := func(t testing.TB) *pbTests.TestApp { return testHub.TestApp }
	const path = "/api/beszel/fork/actions"
	post := func(token string, body map[string]any) beszelTests.ApiScenario {
		headers := map[string]string{"Content-Type": "application/json"}
		if token != "" {
			headers["Authorization"] = token
		}
		return beszelTests.ApiScenario{
			Method: http.MethodPost, URL: path, Body: jsonReader(body), TestAppFactory: testAppFactory, Headers: headers,
		}
	}
	containerStop := map[string]any{"system": allowed.Id, "kind": "container", "target": "abcdef123456", "action": "stop"}

	var scenarios []beszelTests.ApiScenario
	add := func(s beszelTests.ApiScenario, name string, status int, content ...string) {
		s.Name, s.ExpectedStatus, s.ExpectedContent = name, status, content
		scenarios = append(scenarios, s)
	}

	add(post("", containerStop), "no auth", 401, "requires valid")
	add(post(userToken, containerStop), "regular user is refused", 403, "not allowed")

	type invalidCase struct {
		body    map[string]any
		message string
	}
	invalid := map[string]invalidCase{
		"bad action":    {map[string]any{"system": allowed.Id, "kind": "container", "target": "abcdef123456", "action": "kill"}, "Invalid action"},
		"bad kind":      {map[string]any{"system": allowed.Id, "kind": "volume", "target": "abcdef123456", "action": "stop"}, "Invalid kind"},
		"bad container": {map[string]any{"system": allowed.Id, "kind": "container", "target": "../etc", "action": "stop"}, "Invalid container"},
		"bad service":   {map[string]any{"system": allowed.Id, "kind": "service", "target": "a.service;reboot", "action": "stop"}, "Invalid service"},
		"timer unit":    {map[string]any{"system": allowed.Id, "kind": "service", "target": "backup.timer", "action": "stop"}, "Invalid service"},
	}
	for name, c := range invalid {
		add(post(adminToken, c.body), "invalid input: "+name, 400, c.message)
	}
	add(post(outsiderToken, containerStop), "admin who cannot read the system", 404, "wasn't found")
	add(post(adminToken, map[string]any{"system": "nope", "kind": "container", "target": "abcdef123456", "action": "stop"}), "unknown system", 404, "wasn't found")

	refused := post(adminToken, map[string]any{"system": disabled.Id, "kind": "service", "target": "web.service", "action": "restart"})
	refused.AfterTestFunc = func(t testing.TB, app *pbTests.TestApp, _ *http.Response) {
		assert.Empty(t, sent, "validation and refusals never reach the agent")
		assert.Equal(t, 1, auditCount(t, app, ""), "only the refused attempt is logged")
		assert.Equal(t, 1, auditCount(t, app, "refused"))
	}
	add(refused, "agent without the capability", 403, "not enabled")

	ok := post(adminToken, containerStop)
	ok.AfterTestFunc = func(t testing.TB, app *pbTests.TestApp, _ *http.Response) {
		require.Len(t, sent, 1)
		assert.Equal(t, common.ControlRequest{Kind: "container", Target: "abcdef123456", Action: "stop"}, sent[0])
		assert.Equal(t, 1, auditCount(t, app, "ok"))
		record, err := app.FindFirstRecordByFilter("fork_audit", "result = 'ok'")
		require.NoError(t, err)
		assert.Equal(t, "admin@example.com", record.GetString("user_email"))
		assert.Equal(t, "allowed", record.GetString("system_name"))
		assert.Equal(t, "stop", record.GetString("action"))
		assert.Equal(t, "abcdef123456", record.GetString("target"))
	}
	add(ok, "stop a container", 200, `"result":"ok"`)

	restart := post(adminToken, map[string]any{"system": allowed.Id, "kind": "service", "target": "stack-web", "action": "restart"})
	restart.AfterTestFunc = func(t testing.TB, _ *pbTests.TestApp, _ *http.Response) {
		require.Len(t, sent, 2)
		assert.Equal(t, common.ControlRequest{Kind: "service", Target: "stack-web", Action: "restart"}, sent[1])
		agentErr = errors.New("no such container")
	}
	add(restart, "restart a service", 200, `"result":"ok"`)

	failed := post(adminToken, containerStop)
	failed.AfterTestFunc = func(t testing.TB, app *pbTests.TestApp, _ *http.Response) {
		assert.Equal(t, 1, auditCount(t, app, "error"))
		assert.Equal(t, 4, auditCount(t, app, ""), "refused, two ok, one error")
	}
	add(failed, "agent error is reported and logged", 502, "no such container")

	// who may read the audit log
	add(beszelTests.ApiScenario{
		Method: http.MethodGet, URL: "/api/collections/fork_audit/records", TestAppFactory: testAppFactory,
		Headers: map[string]string{"Authorization": adminToken},
	}, "admin reads the audit log", 200, `"action":"stop"`, `"totalItems":4`)
	add(beszelTests.ApiScenario{
		Method: http.MethodGet, URL: "/api/collections/fork_audit/records", TestAppFactory: testAppFactory,
		Headers: map[string]string{"Authorization": userToken},
	}, "regular user cannot read the audit log", 200, `"totalItems":0`)
	add(beszelTests.ApiScenario{
		Method: http.MethodPost, URL: "/api/collections/fork_audit/records", TestAppFactory: testAppFactory,
		Body:    jsonReader(map[string]any{"action": "x"}),
		Headers: map[string]string{"Authorization": adminToken, "Content-Type": "application/json"},
	}, "nobody can write the audit log", 403, "Only superusers")

	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
