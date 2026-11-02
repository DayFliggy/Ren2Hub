package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

func TestDashboardSummaryRouteScopes(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.QuotaData{}))
	oldExport, oldConsume, oldErrorLog := common.DataExportEnabled, common.LogConsumeEnabled, constant.ErrorLogEnabled
	common.DataExportEnabled, common.LogConsumeEnabled, constant.ErrorLogEnabled = true, false, false
	t.Cleanup(func() {
		common.DataExportEnabled, common.LogConsumeEnabled, constant.ErrorLogEnabled = oldExport, oldConsume, oldErrorLog
	})
	today := time.Now().UTC().Truncate(24 * time.Hour).Unix()
	user, userSession := createSystemStatusSession(t, db, 92001, "summary-user", common.RoleCommonUser)
	admin, adminSession := createSystemStatusSession(t, db, 92002, "summary-admin", common.RoleAdminUser)
	root, rootSession := createSystemStatusSession(t, db, 92003, "summary-root", common.RoleRootUser)
	require.NoError(t, db.Create(&[]model.QuotaData{
		{UserID: user.Id, CreatedAt: today, ModelName: "public-model", Quota: 10, Count: 1},
		{UserID: admin.Id, CreatedAt: today, ModelName: "public-model", Quota: 20, Count: 2},
		{UserID: root.Id, CreatedAt: today, ModelName: "public-model", Quota: 30, Count: 3},
		{UserID: 92004, CreatedAt: today, ModelName: "public-model", Quota: 40, Count: 4},
	}).Error)
	pat := func(user model.User, name string, scopes ...string) string {
		t.Helper()
		raw := model.AccessTokenPrefix + "dashboard-summary-" + name
		token := model.UserAccessToken{UserId: user.Id, Name: name, TokenHash: model.AccessTokenFingerprint(raw), ExpiresAt: time.Now().Add(time.Hour).Unix(), CreatedAt: time.Now().Unix()}
		require.NoError(t, token.SetScopes(scopes))
		require.NoError(t, db.Create(&token).Error)
		return raw
	}
	adminPersonal := pat(admin, "personal", "usage:read")
	adminSite := pat(admin, "site", "log:read")
	rootSite := pat(root, "root-site", "log:read")
	userSite := pat(user, "user-site", "log:read") // Forged stale grant still cannot exceed the live role.
	call := func(path, token string) (*httptest.ResponseRecorder, service.DashboardSummary) {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		var response struct {
			Success bool                     `json:"success"`
			Data    service.DashboardSummary `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		if recorder.Code == http.StatusOK {
			require.True(t, response.Success, recorder.Body.String())
			require.Contains(t, recorder.Header().Get("Cache-Control"), "no-store")
		}
		return recorder, response.Data
	}
	const self = "/api/data/self/summary?range=today&include_comparison=false&user_id=92003&scope=site&username=%25"
	const site = "/api/data/summary?range=today&include_comparison=false"
	for _, tc := range []struct {
		name, path, token string
		status            int
		quota             int64
	}{
		{"anonymous cannot read site", site, "", http.StatusUnauthorized, 0},
		{"user cannot read site", site, userSession, http.StatusForbidden, 0},
		{"user remains personal despite query", self, userSession, http.StatusOK, 10},
		{"admin personal", self, adminSession, http.StatusOK, 20},
		{"root personal", self, rootSession, http.StatusOK, 30},
		{"admin site includes root and deleted history", site, adminSession, http.StatusOK, 100},
		{"root site", site, rootSession, http.StatusOK, 100},
		{"personal PAT has no site access", site, adminPersonal, http.StatusForbidden, 0},
		{"personal PAT remains personal", self, adminPersonal, http.StatusOK, 20},
		{"site PAT has no implicit personal grant", self, adminSite, http.StatusForbidden, 0},
		{"admin site PAT", site, adminSite, http.StatusOK, 100},
		{"root site PAT", site, rootSite, http.StatusOK, 100},
		{"forged user site grant cannot elevate", site, userSite, http.StatusForbidden, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, summary := call(tc.path, tc.token)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.quota, summary.KPI.TotalQuota)
			for _, field := range []string{`"user_id"`, `"username"`, `"token_id"`, `"channel_id"`} {
				require.NotContains(t, response.Body.String(), field)
			}
		})
	}
	t.Run("demotion invalidates site scope for existing session and PAT", func(t *testing.T) {
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", admin.Id).Update("role", common.RoleCommonUser).Error)
		for _, token := range []string{adminSession, adminSite} {
			response, _ := call(site, token)
			require.Equal(t, http.StatusForbidden, response.Code, response.Body.String())
		}
	})
}
