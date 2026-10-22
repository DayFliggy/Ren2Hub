package middleware

import (
	"net/http"
	"os"
	"path/filepath"
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

// POST /api/user/aff_transfer 只接受浏览器会话，wallet 的描述不能再写 PAT 可以转出返利；改路由或改文案的人会在这里同时看到另一半。
func TestWalletScopeDescriptionsMatchSessionOnlyAffTransfer(t *testing.T) {
	require.Equal(t, accessTokenSessionRule, accessTokenRouteRules["POST /api/user/aff_transfer"])
	descriptions := make(map[string]string)
	for _, resource := range service.AccessTokenScopeDictionary() {
		if resource.Resource != "wallet" {
			continue
		}
		for _, action := range resource.Actions {
			descriptions[action.Action] = action.DescriptionKey
		}
	}
	require.Equal(t, map[string]string{
		"read":  "View balance, top-up records, and referral code.",
		"write": "Top up and check in.",
	}, descriptions)
}

// 前端 zh-CN 的 permissions.json 是权限目录文案的唯一译文来源：每个 label_key、description_key 都要有译文，也不留目录里已不存在的条目。
func TestPermissionCatalogTextMatchesZhCNTranslations(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "frontend", "src", "i18n", "locales", "zh-CN", "server-messages", "permissions.json"))
	require.NoError(t, err)
	var translations map[string]string
	require.NoError(t, common.Unmarshal(raw, &translations))
	used := make(map[string]bool)
	for _, resource := range service.AccessTokenScopeDictionary() {
		keys := []string{resource.LabelKey}
		for _, action := range resource.Actions {
			keys = append(keys, action.LabelKey, action.DescriptionKey)
		}
		for _, key := range keys {
			require.NotEmpty(t, translations[key], "permissions.json lacks %q (%s)", key, resource.Resource)
			used[key] = true
		}
	}
	for key := range translations {
		require.True(t, used[key], "permissions.json keeps %q, which no permission uses", key)
	}
}
