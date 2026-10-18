package middleware

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEveryScopedDashboardRouteHasAnIssuableScope(t *testing.T) {
	scopes := make(map[string]bool)
	for _, resource := range service.AccessTokenScopeDictionary() {
		for _, action := range resource.Actions {
			scopes[resource.Resource+":"+action.Action] = true
		}
	}
	for path, rule := range accessTokenRouteRules {
		if rule.kind == accessTokenRuleScope {
			require.True(t, scopes[rule.scope], "%s requires unissuable scope %q", path, rule.scope)
		}
	}
}

func TestTopUpReadRoutesAcceptOnlyGrantedBillingScope(t *testing.T) {
	setupDashboardAuthMiddlewareTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(model.DB))
	admin := createMiddlewarePATUser(t, "billing-route-admin", "billing-test-legacy")
	require.NoError(t, model.DB.Model(admin).Update("role", common.RoleAdminUser).Error)
	normalized, err := service.NormalizeAccessTokenScopes(admin.Id, common.RoleAdminUser, []string{"billing:read"})
	require.NoError(t, err)
	billing, _ := createMiddlewareScopedToken(t, admin.Id, 0, normalized...)
	profile, _ := createMiddlewareScopedToken(t, admin.Id, 0, "profile:read")
	router := gin.New()
	for _, path := range []string{"/api/user/topup/stats", "/api/user/topup/admin/detail/:trade_no"} {
		router.GET(path, AdminAuth(), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	}
	for _, path := range []string{"/api/user/topup/stats", "/api/user/topup/admin/detail/local-test-order"} {
		require.Equal(t, http.StatusNoContent, middlewareBearerRequest(router, path, billing).Code)
		require.Equal(t, http.StatusForbidden, middlewareBearerRequest(router, path, profile).Code)
	}
}
