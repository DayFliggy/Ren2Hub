package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerGatewayRoutes(apiRouter *gin.RouterGroup) {
	gateway := apiRouter.Group("/gateway", middleware.DisableCache())
	gateway.GET("/self", middleware.UserAuth(), controller.GetGatewayUserAdmission)
	gateway.GET("/self/runtime", middleware.UserAuth(), controller.GetGatewayUserRuntime)
	gateway.GET("/config", middleware.RootAuth(), controller.GetGatewayConfig)
	gateway.GET("/metrics", middleware.RootAuth(), middleware.CriticalRateLimit(), controller.GetGatewayMetrics)
	gateway.PUT("/config", middleware.RootAuth(), middleware.CriticalRateLimit(), controller.UpdateGatewayConfig)
	gateway.GET("/users/:id", middleware.AdminAuth(), controller.GetGatewayUserAdmission)
	gateway.PUT("/users/:id", middleware.AdminAuth(), middleware.CriticalRateLimit(), controller.UpdateGatewayUserAdmission)
	gateway.GET("/runtime/users/:id", middleware.AdminAuth(), controller.GetGatewayUserRuntime)
	gateway.GET("/runtime/channels/:id", middleware.AdminAuth(), middleware.RequirePermission(authz.ChannelRead), controller.GetGatewayChannelRuntime)
	gateway.GET("/channels/:id/credentials", middleware.AdminAuth(), middleware.RequirePermission(authz.ChannelRead), controller.ListGatewayChannelCredentials)
	gateway.GET("/runtime/credentials/:id", middleware.AdminAuth(), middleware.RequirePermission(authz.ChannelRead), controller.GetGatewayCredentialRuntime)
}
