package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerPersonalAccessTokenManagementRoutes(parent *gin.RouterGroup) {
	// Keep the first-batch path as an alias; both paths have the same browser-only policy.
	for _, prefix := range []string{"/self/personal-access-tokens", "/personal-access-tokens"} {
		group := parent.Group(prefix, middleware.BrowserSessionOnly(), middleware.DisableCache())
		group.GET("", controller.ListPersonalAccessTokens)
		group.GET("/scopes", controller.GetPersonalAccessTokenScopes)
		group.POST("", middleware.CriticalRateLimit(), controller.CreatePersonalAccessToken)
		group.PATCH("/:id", middleware.CriticalRateLimit(), controller.UpdatePersonalAccessToken)
		group.DELETE("/:id", middleware.CriticalRateLimit(), controller.RevokePersonalAccessToken)
	}
}

// Exact method/path declarations deliberately exclude browser account settings
// and PAT management. New management routes are session-only until declared.
func declarePATRoutes() {
	middleware.DeclarePATRoute("GET", "/api/user/model-manifest", "profile:read")
	middleware.DeclarePATRoute("GET", "/api/token/:id/client-config", "tokens:read")
	middleware.DeclarePATRoute("GET", "/api/log/self/request-diagnostics", "logs:read")
	middleware.DeclarePATRoute("GET", "/api/log/self/requests/:request_id/diagnostics", "logs:read")
	middleware.DeclarePATRoute("GET", "/api/log/request-diagnostics", "ops:read")
	middleware.DeclarePATRoute("GET", "/api/log/requests/:request_id/diagnostics", "ops:read")
	for scope, routes := range map[string][]string{
		"profile:read":             {"GET /api/user/self", "GET /api/user/models", "GET /api/models", "GET /api/gateway/self", "GET /api/gateway/self/runtime", "GET /api/next/dashboard/stats", "GET /api/next/dashboard/distribution", "GET /api/next/dashboard/token-trend", "GET /api/next/dashboard/system-status"},
		"verification:write":       {"GET /api/verify/methods", "POST /api/verify", "POST /api/oauth/state", "GET /api/oauth/:provider", "POST /api/user/passkey/verify/begin", "POST /api/user/passkey/verify/finish"},
		"tokens:read":              {"GET /api/token/", "GET /api/token/search", "GET /api/token/:id", "POST /api/token/:id/key", "POST /api/token/batch/keys"},
		"tokens:write":             {"POST /api/token/", "PUT /api/token/", "DELETE /api/token/:id", "POST /api/token/batch"},
		"routing:read":             {"GET /api/routing/catalog", "GET /api/routing/eligible-channels", "GET /api/routing/profiles", "GET /api/routing/profiles/:id", "POST /api/routing/profiles/:id/preview", "POST /api/routing/tokens/:id/preview"},
		"routing:write":            {"POST /api/routing/profiles", "PUT /api/routing/profiles/:id", "DELETE /api/routing/profiles/:id"},
		"billing:read":             {"GET /api/billing/self/reservations", "GET /api/billing/self/events", "GET /api/user/topup/self", "GET /api/next/orders/self", "GET /api/next/orders/self/:order_no"},
		"logs:read":                {"GET /api/log/self", "GET /api/log/self/export", "GET /api/log/self/stat", "GET /api/log/self/search", "GET /api/log/self/groups", "GET /api/audit/self", "GET /api/data/self", "GET /api/data/flow/self"},
		"users:read":               {"GET /api/user/", "GET /api/user/search", "GET /api/user/:id", "GET /api/user/:id/oauth/bindings", "GET /api/user/2fa/stats", "GET /api/next/admin/users", "GET /api/gateway/users/:id", "GET /api/gateway/runtime/users/:id", "GET /api/next/admin/orders", "GET /api/next/admin/orders/stats", "GET /api/next/admin/orders/:order_no", "GET /api/user/topup"},
		"users:write":              {"POST /api/user/", "PUT /api/user/", "POST /api/user/manage", "DELETE /api/user/:id", "DELETE /api/user/:id/reset_passkey", "DELETE /api/user/:id/2fa", "DELETE /api/user/:id/bindings/:binding_type", "DELETE /api/user/:id/oauth/bindings/:provider_id", "POST /api/next/admin/users", "PUT /api/next/admin/users", "POST /api/next/admin/users/quota", "POST /api/next/admin/users/status/batch", "POST /api/next/admin/users/delete/batch", "POST /api/next/admin/users/:id/status", "PUT /api/gateway/users/:id", "POST /api/user/topup/complete"},
		"channels:read":            {"GET /api/next/admin/channels", "GET /api/next/admin/dashboard/routes", "GET /api/ops/channels", "GET /api/gateway/runtime/channels/:id", "GET /api/gateway/channels/:id/credentials", "GET /api/gateway/runtime/credentials/:id"},
		"channels:write":           {"PUT /api/next/admin/channels"},
		"channels:operate":         {"GET /api/next/admin/channels/test/:id", "GET /api/next/admin/channels/balance/:id", "GET /api/next/admin/channels/fetch_models/:id", "POST /api/next/admin/channels/status/batch", "POST /api/next/admin/channels/:id/status"},
		"channels:sensitive_write": {"POST /api/next/admin/channels", "POST /api/next/admin/channels/fetch_models"},
		"channels:key_read":        {"POST /api/channel/:id/key"},
		"models:read":              {"GET /api/models/", "GET /api/models/search", "GET /api/models/:id", "GET /api/models/missing", "GET /api/models/sync_upstream/preview", "GET /api/vendors/", "GET /api/vendors/search", "GET /api/vendors/:id"},
		"models:write":             {"POST /api/models/", "PUT /api/models/", "DELETE /api/models/:id", "POST /api/models/sync_upstream", "POST /api/vendors/", "PUT /api/vendors/", "DELETE /api/vendors/:id"},
		"redemptions:read":         {"GET /api/redemption/", "GET /api/redemption/search", "GET /api/redemption/:id", "GET /api/next/admin/redemptions"},
		"redemptions:write":        {"POST /api/redemption/", "PUT /api/redemption/", "DELETE /api/redemption/:id", "DELETE /api/redemption/invalid", "POST /api/next/admin/redemptions", "POST /api/next/admin/redemptions/delete/batch", "POST /api/next/admin/redemptions/:id/status"},
		"options:read":             {"GET /api/option/", "GET /api/option/catalog", "GET /api/option/model_pricing", "GET /api/option/secret-status", "GET /api/gateway/config", "GET /api/gateway/metrics"},
		"options:write":            {"PUT /api/option/", "PUT /api/option/bulk", "PATCH /api/option/model_pricing", "POST /api/option/model_pricing/preview", "POST /api/option/model_pricing/convert", "POST /api/option/model_pricing/simulate", "PUT /api/gateway/config", "POST /api/ratio_sync/fetch"},
		"audit:read":               {"GET /api/audit"},
		"ops:read":                 {"GET /api/ops/overview", "GET /api/ops/requests", "GET /api/status/test", "GET /api/log/", "GET /api/log/export", "GET /api/log/stat", "GET /api/log/search", "GET /api/log/groups", "GET /api/data/", "GET /api/data/users", "GET /api/data/flow", "GET /api/task/", "GET /api/mj/"},
		"tickets:read":             {"GET /api/next/admin/tickets", "GET /api/next/admin/tickets/summary", "GET /api/next/admin/tickets/:id", "GET /api/next/admin/tickets/attachments/:attachment_id"},
		"tickets:reply":            {"POST /api/next/admin/tickets/:id/messages"},
		"tickets:manage":           {"GET /api/next/admin/tickets/agents", "PATCH /api/next/admin/tickets/:id/status", "PATCH /api/next/admin/tickets/:id/assignee"},
		"plugins:bind":             {"GET /api/task_plugin_options"},
	} {
		for _, route := range routes {
			for i := range route {
				if route[i] == ' ' {
					middleware.DeclarePATRoute(route[:i], route[i+1:], scope)
					break
				}
			}
		}
	}
	for _, route := range channelPermissionRoutes {
		middleware.DeclarePATRoute(route.method, "/api/channel"+route.path, "channels:"+route.permission.Action)
	}
}
