//go:build testing

package hub_test

import (
	"net/http"
	"testing"

	beszelTests "github.com/henrygd/beszel/internal/tests"
	pbTests "github.com/pocketbase/pocketbase/tests"
	"github.com/stretchr/testify/require"
)

func TestForkDisableUpdate(t *testing.T) {
	t.Setenv("CHECK_UPDATES", "true")
	t.Setenv("FORK_DISABLE_UPDATE", "true")

	testHub, user := beszelTests.GetHubWithUser(t)
	defer testHub.Cleanup()
	token, err := user.NewAuthToken()
	require.NoError(t, err)
	testAppFactory := func(t testing.TB) *pbTests.TestApp { return testHub.TestApp }
	headers := map[string]string{"Authorization": token}

	for _, scenario := range []beszelTests.ApiScenario{
		{
			Name: "update check is not served even with CHECK_UPDATES", Method: http.MethodGet, URL: "/api/beszel/update",
			Headers: headers, ExpectedStatus: 404, NotExpectedContent: []string{`"v":`}, TestAppFactory: testAppFactory,
		},
		{
			Name: "the UI is told not to check", Method: http.MethodGet, URL: "/api/beszel/info",
			Headers: headers, ExpectedStatus: 200, ExpectedContent: []string{`"key":`},
			NotExpectedContent: []string{`"cu":true`}, TestAppFactory: testAppFactory,
		},
	} {
		scenario.Test(t)
	}
}
