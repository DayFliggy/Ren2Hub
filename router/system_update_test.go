package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type systemReleaseTransport struct{}

func (systemReleaseTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := `{"tag_name":"v1.0.2"}`
	if strings.HasSuffix(req.URL.Path, "/releases") {
		body = `[{"tag_name":"v1.0.1"}]`
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
}

func TestSystemUpdateRoutesRequireRootSession(t *testing.T) {
	_, db := setupDashboardSystemStatusRouter(t)
	engine := gin.New()
	SetRouter(engine, WebAssets{})
	oldVersion, oldTransport := common.Version, http.DefaultTransport
	common.Version, http.DefaultTransport = "v1.0.2", systemReleaseTransport{}
	t.Cleanup(func() { common.Version, http.DefaultTransport = oldVersion, oldTransport })
	_, user := createSystemStatusSession(t, db, 91401, "update-user", common.RoleCommonUser)
	_, admin := createSystemStatusSession(t, db, 91402, "update-admin", common.RoleAdminUser)
	root, session := createSystemStatusSession(t, db, 91403, "update-root", common.RoleRootUser)
	raw, err := common.GenerateRandomCharsKey(43)
	require.NoError(t, err)
	raw = model.AccessTokenPrefix + raw
	pat := &model.UserAccessToken{Name: "system update", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
	require.NoError(t, pat.SetScopes([]string{"profile:read"}))
	require.NoError(t, model.CreateUserAccessToken(root.Id, pat, service.AccessTokenMaxPerUser))

	// Intercept only this test process's restart, after every other route check.
	terminated := make(chan os.Signal, 1)
	signal.Notify(terminated, syscall.SIGTERM)
	defer signal.Stop(terminated)
	for _, tc := range []struct {
		method, route string
		rootStatus    int
	}{
		{"GET", "version", 200}, {"GET", "check-updates?force=true", 200},
		{"GET", "rollback-versions", 200}, {"POST", "update", 400},
		{"POST", "rollback", 400}, {"POST", "restart", 200},
	} {
		for _, identity := range []struct {
			token  string
			status int
		}{
			{"", 401}, {user, 403}, {admin, 403}, {raw, 403}, {session, tc.rootStatus},
		} {
			req := httptest.NewRequest(tc.method, "/api/admin/system/"+tc.route, strings.NewReader(`{"version":"1.0.1"}`))
			req.Header.Set("Content-Type", "application/json")
			if identity.token != "" {
				req.Header.Set("Authorization", "Bearer "+identity.token)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, req)
			require.Equal(t, identity.status, response.Code, "%s %s", tc.method, tc.route)
			if identity.token == session && tc.route == "version" {
				require.JSONEq(t, `{"success":true,"message":"","data":{"version":"v1.0.2","build_type":"source"}}`, response.Body.String())
			}
		}
	}
	select {
	case <-terminated:
	case <-time.After(3 * time.Second):
		t.Fatal("restart did not signal this process")
	}
}
