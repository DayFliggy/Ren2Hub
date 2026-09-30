package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func RegisterTaskPluginManagementRoutes(api *gin.RouterGroup) {
	plugins := api.Group("/plugin/task", middleware.RootAuth(), middleware.DisableCache())
	plugins.GET("", controller.ListTaskPlugins)
	plugins.POST("", controller.UploadTaskPlugin)
	plugins.PUT("", controller.UploadTaskPlugin)
	plugins.POST("/preview", controller.PreviewTaskPlugin)
	plugins.GET("/runtime/status", controller.GetTaskPluginRuntime)
	plugins.GET("/marketplace/sources", controller.GetTaskPluginMarketplaceSources)
	plugins.PUT("/marketplace/sources", controller.UpdateTaskPluginMarketplaceSources)
	plugins.GET("/:key", controller.GetTaskPlugin)
	plugins.GET("/:key/icon", controller.GetTaskPluginIcon)
	plugins.GET("/:key/versions", controller.GetTaskPluginVersions)
	plugins.POST("/:key/activate", controller.ActivateTaskPlugin)
	plugins.POST("/:key/status", controller.SetTaskPluginStatus)
	plugins.POST("/:key/dryrun", controller.DryRunTaskPlugin)
	plugins.DELETE("/:key/versions/:version", controller.DeleteTaskPluginVersion)
	api.GET("/task_plugin_options", middleware.AdminAuth(), middleware.RequirePermission(authz.TaskPluginBind), controller.GetTaskPluginOptions)
}
