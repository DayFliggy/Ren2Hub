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
	// The guest home page shows only the model count, behind the /api/pricing gate.
	middleware.DeclareAnyAccessTokenRoute(http.MethodGet, joinPaths(api.BasePath(), "/pricing/count"))
	api.GET("/pricing/count", middleware.HeaderNavModuleAuth("pricing"), controller.GetPricingCount)
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
	handlePermissionRoute(admin, http.MethodPost, "/:id/messages", authz.TicketReply, middleware.UserCriticalRateLimit("ticket-admin"), controller.AddAdminTicketMessage)
	handlePermissionRoute(admin, http.MethodPatch, "/:id/status", authz.TicketManage, controller.UpdateAdminTicketStatus)
	handlePermissionRoute(admin, http.MethodPatch, "/:id/assignee", authz.TicketManage, controller.AssignAdminTicket)
	affiliate := api.Group("/user/affiliate", middleware.UserAuth(), middleware.DisableCache())
	handleSessionRoute(affiliate, http.MethodGet, "", controller.GetAffiliateInfo)
	handleSessionRoute(affiliate, http.MethodGet, "/records", controller.GetUserAffiliateRecords)
	handleSessionRoute(affiliate, http.MethodGet, "/invitees", controller.GetAffiliateInvitees)
	handleSessionRoute(affiliate, http.MethodPost, "/transfer", middleware.UserCriticalRateLimit("aff-transfer"), controller.TransferAffiliateQuota)
	affiliateAdmin := api.Group("/admin/affiliate", middleware.AdminAuth(), middleware.DisableCache())
	affiliateAdmin.GET("/records", controller.GetAffiliateRecords)
	affiliateAdmin.GET("/users/:id", controller.GetAffiliateUserPolicy)
	affiliateAdmin.PUT("/users/:id", controller.UpdateAffiliateUserPolicy)
	systemStatus := api.Group("/user/system-status", middleware.UserAuth(), middleware.DisableCache())
	handleSessionRoute(systemStatus, http.MethodGet, "", middleware.DashboardStatusRateLimit(), controller.GetDashboardSystemStatus)
	orders := api.Group("/user/topup", middleware.UserAuth(), middleware.DisableCache())
	handleSessionRoute(orders, http.MethodGet, "/detail/:trade_no", controller.GetTopUpDetail)
	orders.GET("/stats", middleware.AdminAuth(), controller.GetTopUpStatistics)
	orders.GET("/ranking", middleware.AdminAuth(), controller.GetTopUpRanking)
	orders.GET("/admin/detail/:trade_no", middleware.AdminAuth(), controller.GetAdminTopUpDetail)

	pay := api.Group("/user/gmpay", middleware.UserAuth())
	handleSessionRoute(pay, http.MethodPost, "/pay", middleware.CriticalRateLimit(), controller.GmpayPay)
	handleSessionRoute(pay, http.MethodGet, "/orders/:trade_no", controller.GetGmpayOrder)
	// epusdt 对未声明 payment_type=Epay 的订单只发 POST JSON 回调，GET 通知已退役。
	api.POST("/user/gmpay/notify", controller.GmpayNotify)
	settings := api.Group("/option/gmpay", middleware.RootAuth())
	handleSessionRoute(settings, http.MethodGet, "/config", controller.GetGmpayConfig)
	handleSessionRoute(settings, http.MethodPost, "/save", controller.SaveGmpayConfig)
	handleSessionRoute(settings, http.MethodPost, "/probe", middleware.CriticalRateLimit(), controller.ProbeGmpay)
}
