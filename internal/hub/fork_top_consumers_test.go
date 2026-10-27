//go:build testing

package hub_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	beszelTests "github.com/henrygd/beszel/internal/tests"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTopConsumersEndpoint(t *testing.T) {
	hub, user := beszelTests.GetHubWithUser(t)
	defer hub.Cleanup()
	userToken, err := user.NewAuthToken()
	require.NoError(t, err)

	other, err := beszelTests.CreateUser(hub, "other@example.com", "password123")
	require.NoError(t, err)
	otherToken, err := other.NewAuthToken()
	require.NoError(t, err)

	small, err := beszelTests.CreateRecord(hub, "systems", map[string]any{
		"name": "small", "host": "10.0.0.1", "port": "45876", "users": []string{user.Id},
	})
	require.NoError(t, err)
	big, err := beszelTests.CreateRecord(hub, "systems", map[string]any{
		"name": "big", "host": "10.0.0.2", "port": "45876", "users": []string{user.Id},
	})
	require.NoError(t, err)
	hidden, err := beszelTests.CreateRecord(hub, "systems", map[string]any{
		"name": "hidden", "host": "10.0.0.3", "port": "45876", "users": []string{other.Id},
	})
	require.NoError(t, err)

	// the big system has 8 cores, the small one 1: 50% of 1 core is less than 10% of 8 cores
	for system, cores := range map[*core.Record]int{small: 1, big: 8, hidden: 2} {
		_, err = beszelTests.CreateRecord(hub, "system_details", map[string]any{"system": system.Id, "cores": cores})
		require.NoError(t, err)
	}
	stats := func(system *core.Record, created time.Time, body string) {
		record, err := beszelTests.CreateRecord(hub, "container_stats", map[string]any{
			"system": system.Id, "type": "1m", "stats": json.RawMessage(body),
		})
		require.NoError(t, err)
		// created is an autodate field, so it has to be backdated directly
		_, err = hub.DB().NewQuery("UPDATE container_stats SET created = {:created} WHERE id = {:id}").
			Bind(dbx.Params{"created": created.UTC().Format("2006-01-02 15:04:05.000Z"), "id": record.Id}).Execute()
		require.NoError(t, err)
	}
	now := time.Now()
	stats(small, now.Add(-5*time.Minute), `[{"n":"cache","c":50,"m":900}]`)
	stats(small, now.Add(-4*time.Minute), `[{"n":"cache","c":30,"m":900}]`)
	stats(big, now.Add(-5*time.Minute), `[{"n":"worker","c":10,"m":100}]`)
	stats(big, now.Add(-3*time.Hour), `[{"n":"worker","c":99,"m":5000}]`) // outside the 1h window
	stats(hidden, now.Add(-5*time.Minute), `[{"n":"secret","c":90,"m":9000}]`)

	testAppFactory := func(t testing.TB) *pbTests.TestApp { return hub.TestApp }
	auth := map[string]string{"Authorization": userToken}

	scenarios := []beszelTests.ApiScenario{
		{
			Name: "no auth", Method: http.MethodGet, URL: "/api/beszel/fork/top-consumers",
			ExpectedStatus: 401, ExpectedContent: []string{"requires valid"}, TestAppFactory: testAppFactory,
		},
		{
			Name: "cpu ranks by cores in use, not by percent", Method: http.MethodGet,
			URL: "/api/beszel/fork/top-consumers?metric=cpu&window=1h", Headers: auth, ExpectedStatus: 200,
			// worker: 10% of 8 cores = 0.8 cores; cache: avg 40% of 1 core = 0.4 cores
			ExpectedContent: []string{`"n":"worker"`, `"n":"cache"`, `"c":40`}, NotExpectedContent: []string{"secret"},
			TestAppFactory: testAppFactory,
			AfterTestFunc: func(t testing.TB, app *pbTests.TestApp, res *http.Response) {
				var body struct {
					Items []struct {
						Name string  `json:"n"`
						Cpu  float64 `json:"c"`
					} `json:"items"`
				}
				require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
				require.Len(t, body.Items, 2)
				assert.Equal(t, "worker", body.Items[0].Name)
				assert.InDelta(t, 10, body.Items[0].Cpu, 0.001, "records outside the window are ignored")
			},
		},
		{
			Name: "memory ranks by MiB", Method: http.MethodGet,
			URL: "/api/beszel/fork/top-consumers?metric=memory&window=1h&limit=1", Headers: auth, ExpectedStatus: 200,
			ExpectedContent: []string{`"n":"cache"`}, NotExpectedContent: []string{`"n":"worker"`}, TestAppFactory: testAppFactory,
		},
		{
			Name: "other user only sees their own systems", Method: http.MethodGet,
			URL: "/api/beszel/fork/top-consumers", Headers: map[string]string{"Authorization": otherToken}, ExpectedStatus: 200,
			ExpectedContent: []string{`"n":"secret"`}, NotExpectedContent: []string{"cache", "worker"}, TestAppFactory: testAppFactory,
		},
		{
			Name: "bad metric", Method: http.MethodGet, URL: "/api/beszel/fork/top-consumers?metric=disk", Headers: auth,
			ExpectedStatus: 400, ExpectedContent: []string{"Metric must be"}, TestAppFactory: testAppFactory,
		},
		{
			Name: "bad window", Method: http.MethodGet, URL: "/api/beszel/fork/top-consumers?window=5y", Headers: auth,
			ExpectedStatus: 400, ExpectedContent: []string{"Invalid window"}, TestAppFactory: testAppFactory,
		},
		{
			Name: "bad limit", Method: http.MethodGet, URL: "/api/beszel/fork/top-consumers?limit=999", Headers: auth,
			ExpectedStatus: 400, ExpectedContent: []string{"Limit must be"}, TestAppFactory: testAppFactory,
		},
	}
	for _, scenario := range scenarios {
		scenario.Test(t)
	}
}
