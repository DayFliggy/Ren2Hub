package router

import (
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func SetRouter(router *gin.Engine, assets WebAssets) {
	router.Use(middleware.RecordRelayRequestMetrics())
	SetApiRouter(router)
	SetDashboardRouter(router)
	SetRelayRouter(router)
	SetVideoRouter(router)
	SetTaskRouter(router)
	router.Use(SetPluginRouter(router))
	SetWebRouter(router, assets)
}
