package router

import (
	"embed"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

// retiredAPIPrefixes are API groups Ren2Hub removed on purpose: in-site
// subscriptions and the Waffo Pancake subscription products, io.net
// deployments, the multi-instance list, business groups and supplier
// subscription (Codex) accounts. The frozen upstream still registers most of
// them, and since upstream 8c8940c99 uploaded plugins may claim host
// namespaces, so they stay 404 here instead of becoming plugin routes.
var retiredAPIPrefixes = []string{
	"/api/subscription", "/api/deployments", "/api/system-info", "/api/group",
	"/api/user/groups", "/api/user/self/groups", "/api/token/auto-groups",
	"/api/option/rest_model_ratio", "/api/codex",
	"/api/option/waffo-pancake/subscription-product",
	"/api/option/waffo-pancake/subscription-product-options",
}

func retiredFrontendPath(path string) bool {
	for _, prefix := range append([]string{"/market", "/console/market", "/pg", "/next", "/playground", "/chat", "/subscription", "/plan-management", "/plans", "/deployments", "/console/subscription", "/console/plan-management", "/console/plans", "/console/playground", "/console/chat", "/console/deployments"}, retiredAPIPrefixes...) {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return retiredChannelCodexPath(path)
}

// retiredChannelCodexPath matches /api/channel/:id/codex/..., whose prefix
// contains the channel ID.
func retiredChannelCodexPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/api/channel/")
	if !ok {
		return false
	}
	_, tail, ok := strings.Cut(rest, "/")
	return ok && (tail == "codex" || strings.HasPrefix(tail, "codex/"))
}

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "frontend/embed-dist")
	compress := gzip.Gzip(gzip.DefaultCompression)

	router.NoRoute(
		func(c *gin.Context) {
			if retiredFrontendPath(c.Request.URL.Path) {
				c.AbortWithStatus(http.StatusNotFound)
			}
		},
		pluginDispatcher,
		middleware.RouteTag("web"),
		func(c *gin.Context) {
			// ServeFrontendFiles sends build files already compressed.
			if !frontendFS.Exists("/", c.Request.URL.Path) {
				compress(c)
			}
		},
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(frontendFS),
		middleware.Cache(),
		middleware.ServeFrontendFiles(frontendFS),
		func(c *gin.Context) {
			if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") || strings.HasPrefix(c.Request.URL.Path, "/pg") {
				controller.RelayNotFound(c)
				return
			}
			c.Header("Cache-Control", "no-cache")
			c.Data(http.StatusOK, "text/html; charset=utf-8", assets.IndexPage)
		},
	)
}
