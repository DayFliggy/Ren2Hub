package router

import (
	"net/http"

	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func handleSessionRoute(group *gin.RouterGroup, method, path string, handlers ...gin.HandlerFunc) {
	middleware.DeclareSessionOnlyAccessTokenRoute(method, joinPaths(group.BasePath(), path))
	group.Handle(method, path, handlers...)
}

func registerExtensionRoutes(api *gin.RouterGroup) {
	user := api.Group("/tickets", middleware.UserAuth())
	handleSessionRoute(user, http.MethodGet, "", controller.ListTickets)
	handleSessionRoute(user, http.MethodPost, "", middleware.CriticalRateLimit(), controller.CreateTicket)
	handleSessionRoute(user, http.MethodGet, "/attachments/:attachment_id", controller.DownloadTicketAttachment)
	handleSessionRoute(user, http.MethodGet, "/:id", controller.GetTicket)
	handleSessionRoute(user, http.MethodPost, "/:id/messages", middleware.CriticalRateLimit(), controller.AddTicketMessage)
	handleSessionRoute(user, http.MethodPatch, "/:id/status", controller.UpdateTicketStatus)
	admin := api.Group("/admin/tickets", middleware.AdminAuth())
	handlePermissionRoute(admin, http.MethodGet, "", authz.TicketRead, controller.ListAdminTickets)
	handlePermissionRoute(admin, http.MethodGet, "/summary", authz.TicketRead, controller.GetAdminTicketSummary)
	handlePermissionRoute(admin, http.MethodGet, "/agents", authz.TicketManage, controller.ListAdminTicketAgents)
	handlePermissionRoute(admin, http.MethodGet, "/attachments/:attachment_id", authz.TicketRead, controller.DownloadAdminTicketAttachment)
	handlePermissionRoute(admin, http.MethodGet, "/:id", authz.TicketRead, controller.GetAdminTicket)
	handlePermissionRoute(admin, http.MethodPost, "/:id/messages", authz.TicketReply, middleware.CriticalRateLimit(), controller.AddAdminTicketMessage)
	handlePermissionRoute(admin, http.MethodPatch, "/:id/status", authz.TicketManage, controller.UpdateAdminTicketStatus)
	handlePermissionRoute(admin, http.MethodPatch, "/:id/assignee", authz.TicketManage, controller.AssignAdminTicket)
	pay := api.Group("/user/gmpay", middleware.UserAuth())
	handleSessionRoute(pay, http.MethodPost, "/pay", middleware.CriticalRateLimit(), controller.GmpayPay)
	handleSessionRoute(pay, http.MethodGet, "/orders/:trade_no", controller.GetGmpayOrder)
	api.GET("/user/gmpay/notify", controller.GmpayNotify)
	api.POST("/user/gmpay/notify", controller.GmpayNotify)
	settings := api.Group("/option/gmpay", middleware.RootAuth())
	handleSessionRoute(settings, http.MethodGet, "/config", controller.GetGmpayConfig)
	handleSessionRoute(settings, http.MethodPost, "/save", controller.SaveGmpayConfig)
	handleSessionRoute(settings, http.MethodPost, "/probe", middleware.CriticalRateLimit(), controller.ProbeGmpay)
}
