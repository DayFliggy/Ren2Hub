package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGmpayRoutesEnforceAuthenticationAndRootConfiguration(t *testing.T) {
	setupRelayRouterTestDB(t)
	previousMemory := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = previousMemory })
	access := map[int]string{}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		name := fmt.Sprintf("gmpay-role-%d", role)
		user := model.User{Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled}
		require.NoError(t, model.DB.Create(&user).Error)
		access[role] = createRouterBrowserSession(t, user.Id)
	}
	engine := gin.New()
	SetApiRouter(engine)
	call := func(role int, method, path, body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		if role != 0 {
			request.Header.Set("Authorization", "Bearer "+access[role])
		}
		engine.ServeHTTP(recorder, request)
		return recorder
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/next/wallet/gmpay/config"},
		{http.MethodPost, "/api/next/wallet/gmpay/quote"},
		{http.MethodPost, "/api/next/wallet/gmpay/topup"},
		{http.MethodPost, "/api/user/gmpay/amount"},
		{http.MethodPost, "/api/user/gmpay/pay"},
		{http.MethodPost, "/api/option/gmpay/probe"},
		{http.MethodPost, "/api/option/gmpay/save"},
	} {
		t.Run("anonymous_"+route.path, func(t *testing.T) {
			assert.Equal(t, http.StatusUnauthorized, call(0, route.method, route.path, `{}`).Code)
		})
	}
	for _, path := range []string{"/api/option/gmpay/probe", "/api/option/gmpay/save"} {
		for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
			assert.Equal(t, http.StatusForbidden, call(role, http.MethodPost, path, `{}`).Code,
				"role=%d path=%s", role, path)
		}
		response := call(common.RoleRootUser, http.MethodPost, path, `{}`)
		assert.Equal(t, http.StatusOK, response.Code)
		assert.Contains(t, response.Body.String(), `"success":false`, "root reaches input validation")
	}
	assert.Equal(t, http.StatusForbidden, call(common.RoleCommonUser, http.MethodGet,
		"/api/next/admin/orders/GP-private-order", "").Code)
	var optionCount int64
	require.NoError(t, model.DB.Model(&model.Option{}).Where("key LIKE ?", "Gmpay%").Count(&optionCount).Error)
	assert.Zero(t, optionCount, "denied and invalid requests must not write merchant configuration")
}

func TestGmpayAnonymousCallbackRejectsUnverifiedPayloads(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.TopUp{}, &model.GmpayOrder{}, &model.GmpayMerchantVersion{}))
	engine := gin.New()
	SetApiRouter(engine)
	for _, test := range []struct {
		name, method, contentType, body string
		status                          int
	}{
		{"get", http.MethodGet, "application/json", `{}`, http.StatusBadRequest},
		{"unsigned", http.MethodPost, "application/json", `{"order_id":"GP-unverified","status":2}`, http.StatusOK},
		{"duplicate_keys", http.MethodPost, "application/json", `{"status":2,"status":1}`, http.StatusBadRequest},
		{"form", http.MethodPost, "application/x-www-form-urlencoded", "status=2", http.StatusBadRequest},
		{"oversized", http.MethodPost, "application/json", strings.Repeat(" ", 16385), http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, "/api/user/gmpay/notify", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, test.status, response.Code)
			assert.Equal(t, "fail", response.Body.String())
		})
	}
}
