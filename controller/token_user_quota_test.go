package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenUserQuotaQueryCreationAndUpdate(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	user := model.User{Username: "query-owner", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	for _, tc := range []struct {
		name          string
		value         any
		present, want bool
	}{
		{name: "default", want: true},
		{name: "explicit-on", value: true, present: true, want: true},
		{name: "explicit-off", value: false, present: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := gin.H{"name": tc.name, "unlimited_quota": true, "expired_time": -1}
			if tc.present {
				body["allow_user_quota_query"] = tc.value
			}
			c, response := newAuthenticatedContext(t, http.MethodPost, "/api/token/", body, user.Id)
			AddToken(c)
			require.True(t, decodeAPIResponse(t, response).Success, response.Body.String())
			var token model.Token
			require.NoError(t, db.Where("name = ?", tc.name).First(&token).Error)
			assert.Equal(t, tc.want, token.AllowUserQuotaQuery)
		})
	}
	for _, field := range []string{"allow_user_quota_query", "ALLOW_USER_QUOTA_QUERY"} {
		for _, value := range []any{nil, "false", 1} {
			c, response := newAuthenticatedContext(t, http.MethodPost, "/api/token/", gin.H{"name": "invalid-permission", "unlimited_quota": true, field: value}, user.Id)
			AddToken(c)
			require.False(t, decodeAPIResponse(t, response).Success)
		}
	}
	var invalidCount int64
	require.NoError(t, db.Model(&model.Token{}).Where("name = ?", "invalid-permission").Count(&invalidCount).Error)
	require.Zero(t, invalidCount)
	token := seedToken(t, db, user.Id, "legacy-key", "legacyquerykey")
	assert.False(t, token.AllowUserQuotaQuery)
	for _, tc := range []struct {
		name, query string
		body        gin.H
		want        bool
	}{
		{"enable", "", gin.H{"allow_user_quota_query": true}, true},
		{"omitted", "", gin.H{"name": "renamed"}, true},
		{"status-only", "?status_only=1", gin.H{"status": common.TokenStatusEnabled, "allow_user_quota_query": false}, true},
		{"disable", "", gin.H{"allow_user_quota_query": false}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.body["id"] = token.Id
			c, response := newAuthenticatedContext(t, http.MethodPut, "/api/token/"+tc.query, tc.body, user.Id)
			UpdateToken(c)
			require.True(t, decodeAPIResponse(t, response).Success, response.Body.String())
			require.NoError(t, db.First(token, token.Id).Error)
			assert.Equal(t, tc.want, token.AllowUserQuotaQuery)
		})
	}
	for _, value := range []any{"false", 1, nil} {
		c, response := newAuthenticatedContext(t, http.MethodPut, "/api/token/", gin.H{"id": token.Id, "allow_user_quota_query": value}, user.Id)
		UpdateToken(c)
		assert.False(t, decodeAPIResponse(t, response).Success)
	}
}

func TestTokenUsageOwnerQuotaAndIPContract(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	initNextAdminUserColumns(t)
	user := model.User{Username: "wallet-owner", Status: common.UserStatusEnabled, Quota: 9007199254740993, UsedQuota: 9223372036854775807, ReservedQuota: 300}
	require.NoError(t, db.Create(&user).Error)
	allowed := "192.0.2.0/24\n2001:db8::/32"
	token := model.Token{UserId: user.Id, Key: "usagequeryfixture", Status: common.TokenStatusExpired, ExpiredTime: 1, AllowUserQuotaQuery: true, AllowIps: &allowed, UnlimitedQuota: true}
	require.NoError(t, db.Create(&token).Error)
	router := gin.New()
	require.NoError(t, router.SetTrustedProxies(nil))
	router.GET("/api/usage/token/", middleware.TokenAuthReadOnly(), GetTokenUsage)
	router.GET("/api/log/token", middleware.TokenAuthReadOnly(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := func(path, remote string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer sk-"+token.Key)
		r.RemoteAddr = remote
		response := httptest.NewRecorder()
		router.ServeHTTP(response, r)
		return response
	}
	for _, remote := range []string{"192.0.2.10:1234", "[2001:db8::1]:1234"} {
		response := request("/api/usage/token/", remote)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var body struct {
			Code bool           `json:"code"`
			Data map[string]any `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
		require.True(t, body.Code)
		assert.Nil(t, body.Data["total_available"])
		assert.Equal(t, map[string]any{"total_available": "9007199254740993", "total_used": "9223372036854775807"}, body.Data["user_quota"])
	}
	for _, path := range []string{"/api/usage/token/", "/api/log/token"} {
		assert.Equal(t, http.StatusForbidden, request(path, "198.51.100.1:1234").Code)
		assert.Equal(t, http.StatusForbidden, request(path, "unresolved").Code)
	}
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("available_quota", -123).Error)
	assert.Contains(t, request("/api/usage/token/", "192.0.2.10:1234").Body.String(), `"total_available":"-123"`)
	require.NoError(t, db.Model(&token).Update("allow_user_quota_query", false).Error)
	assert.NotContains(t, request("/api/usage/token/", "192.0.2.10:1234").Body.String(), "user_quota")
	require.NoError(t, db.Model(&token).Update("status", common.TokenStatusExhausted).Error)
	assert.Equal(t, http.StatusNoContent, request("/api/log/token", "192.0.2.10:1234").Code)
	require.NoError(t, db.Model(&token).Update("status", common.TokenStatusDisabled).Error)
	assert.Equal(t, http.StatusUnauthorized, request("/api/log/token", "192.0.2.10:1234").Code)
	require.NoError(t, db.Model(&token).Update("status", common.TokenStatusEnabled).Error)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("status", common.UserStatusDisabled).Error)
	assert.Equal(t, http.StatusForbidden, request("/api/usage/token/", "192.0.2.10:1234").Code)
}

func TestTokenQuotaQueryAuditAndCommandIdentity(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	user := model.User{Username: "quota-audit", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&user).Error)
	token := seedToken(t, db, user.Id, "quota-audit-key", "quota-audit-secret")
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", user.Id); c.Set("role", common.RoleCommonUser); c.Next() }, middleware.TokenOperationAudit())
	router.PUT("/api/token/", UpdateToken)
	body := gin.H{"id": token.Id, "allow_user_quota_query": true, "operation_id": "toggle-query", "expected_version": token.QuotaVersion}
	for _, wantSuccess := range []bool{true, true, false} {
		if !wantSuccess {
			body["allow_user_quota_query"] = false
		}
		c, response := newAuthenticatedContext(t, http.MethodPut, "/api/token/", body, user.Id)
		router.ServeHTTP(response, c.Request)
		assert.Equal(t, wantSuccess, decodeAPIResponse(t, response).Success, response.Body.String())
	}
	var entries []model.AuditLog
	require.NoError(t, db.Order("id").Find(&entries).Error)
	require.Len(t, entries, 3)
	encoded, err := common.Marshal(entries[0])
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "allow_user_quota_query")
	assert.NotContains(t, string(encoded), token.Key)
	assert.NotContains(t, string(encoded), `"allow_user_quota_query":true`)
	assert.Contains(t, string(encoded), strconv.Itoa(token.Id))
}
