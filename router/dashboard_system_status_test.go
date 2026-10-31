package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The traffic source is decided once per process. A host interface with a
// relative net/dev path is host mode with host_config_invalid, so every
// observation in this package carries a non-empty interface and reason for the
// shared projection to withhold.
const dashboardTestTrafficInterface = "eth-status-test"

func TestMain(m *testing.M) {
	if err := os.Setenv("TRAFFIC_HOST_INTERFACE", dashboardTestTrafficInterface); err != nil {
		panic(err)
	}
	if err := os.Setenv("TRAFFIC_HOST_NET_DEV", "relative/net/dev"); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func setupDashboardSystemStatusRouter(t *testing.T) (*gin.Engine, *gorm.DB) {
	t.Helper()
	previousDB, previousLog, previousRedis, previousSecret := model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.Option{}, &model.AuditLog{}, &model.PerfMetric{}))
	model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret = db, db, false, "system-status-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret = previousDB, previousLog, previousRedis, previousSecret
		common.SetMainDatabaseType(previousType)
		require.NoError(t, sqlDB.Close())
	})
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	return engine, db
}

// The in-memory limiter is process-wide, so callers pass ids no other test uses.
func createSystemStatusSession(t *testing.T, db *gorm.DB, id int, name string, role int) (model.User, string) {
	t.Helper()
	user := model.User{Id: id, Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, db.Create(&user).Error)
	session, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "test")
	require.NoError(t, err)
	return user, session.AccessToken
}

func getSystemStatus(engine *gin.Engine, bearer string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "/api/user/system-status", nil)
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response
}

func TestDashboardSystemStatusIsReadableByEverySignedInRole(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	observation, err := service.ReadPerformanceObservation("1h")
	require.NoError(t, err)
	require.Equal(t, dashboardTestTrafficInterface, observation.Traffic.Interface, "the Root observation carries an interface to withhold")
	require.Equal(t, common.TrafficReasonHostConfigInvalid, observation.Traffic.Reason, "the Root observation carries a reason to withhold")
	require.Equal(t, http.StatusUnauthorized, getSystemStatus(engine, "").Code)
	for _, test := range []struct {
		id   int
		name string
		role int
	}{{91001, "status-user", common.RoleCommonUser}, {91002, "status-admin", common.RoleAdminUser}, {91003, "status-root", common.RoleRootUser}} {
		t.Run(test.name, func(t *testing.T) {
			_, bearer := createSystemStatusSession(t, db, test.id, test.name, test.role)
			response := getSystemStatus(engine, bearer)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			body := response.Body.String()
			require.Contains(t, body, `"success":true`)
			require.Contains(t, body, `"success_rate":{"state":`)
			require.Contains(t, response.Header().Get("Cache-Control"), "no-store")
			// Root-only diagnostics never reach the shared card.
			require.Contains(t, body, `"traffic":{"scope":"host_interface"`)
			for _, field := range []string{`"storage"`, `"checks"`, `"threshold"`, `"process"`, `"db_pool"`, `"coverage"`, `"latency_ms"`, `"interface"`, `"reason"`} {
				require.NotContains(t, body, field)
			}
			require.NotContains(t, body, dashboardTestTrafficInterface)
			// Only the coarse dependency verdict, a bare string, is shared.
			require.Regexp(t, `"storage_status":"(ok|critical|stale|unknown)"`, body)
		})
	}
}

func TestDashboardSystemStatusRejectsPersonalAccessTokens(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	user, _ := createSystemStatusSession(t, db, 91004, "status-pat", common.RoleRootUser)
	suffix, err := common.GenerateRandomCharsKey(43)
	require.NoError(t, err)
	raw := model.AccessTokenPrefix + suffix
	token := &model.UserAccessToken{Name: "status token", TokenHash: model.AccessTokenFingerprint(raw), TokenHint: model.AccessTokenHint(raw)}
	require.NoError(t, token.SetScopes([]string{"profile:read"}))
	require.NoError(t, model.CreateUserAccessToken(user.Id, token, service.AccessTokenMaxPerUser))
	response := getSystemStatus(engine, raw)
	require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), `"cpu"`)
}

// The rate limiter is process-wide, so each run (including -count reruns) takes
// user IDs whose buckets no earlier run has filled.
var systemStatusLimitRun atomic.Int32

func TestDashboardSystemStatusLimitsEachUser(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	id := 91100 + 2*int(systemStatusLimitRun.Add(1))
	_, first := createSystemStatusSession(t, db, id, "status-limit-a", common.RoleCommonUser)
	_, second := createSystemStatusSession(t, db, id+1, "status-limit-b", common.RoleCommonUser)
	for i := 0; i < 30; i++ {
		require.Equal(t, http.StatusOK, getSystemStatus(engine, first).Code, "request %d", i+1)
	}
	limited := getSystemStatus(engine, first)
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.NotEmpty(t, limited.Header().Get("Retry-After"))
	require.Equal(t, http.StatusOK, getSystemStatus(engine, second).Code, "limit is per user")
}
