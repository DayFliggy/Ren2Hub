package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"net/http"
)

func RegisterTokenRoutingRoutes(apiRouter *gin.RouterGroup) {
	group := apiRouter.Group("/token", middleware.UserAuth(), middleware.DisableCache())
	handlePermissionRoute(group, http.MethodGet, "/:id/routing", authz.RoutingRead, controller.GetTokenRouting)
	handlePermissionRoute(group, http.MethodGet, "/:id/routing/channels", authz.RoutingRead, controller.GetTokenRoutingChannels)
	handlePermissionRoute(group, http.MethodGet, "/:id/routing/preview", authz.RoutingRead, controller.GetTokenRoutingPreview)
	handlePermissionRoute(group, http.MethodPut, "/:id/routing", authz.RoutingWrite, middleware.CriticalRateLimit(), controller.PutTokenRouting)
}
