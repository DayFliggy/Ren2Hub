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
	registerUpstreamRoutes(api)
	registerAnnouncementRoutes(api)
	system := api.Group("/admin/system", middleware.RootAuth())
	handleSessionRoute(system, http.MethodGet, "/version", controller.SystemUpdates.Version)
	handleSessionRoute(system, http.MethodGet, "/check-updates", controller.SystemUpdates.CheckUpdates)
	handleSessionRoute(system, http.MethodGet, "/rollback-versions", controller.SystemUpdates.RollbackVersions)
	handleSessionRoute(system, http.MethodPost, "/update", controller.SystemUpdates.Update)
	handleSessionRoute(system, http.MethodPost, "/rollback", controller.SystemUpdates.Rollback)
	handleSessionRoute(system, http.MethodPost, "/restart", controller.SystemUpdates.Restart)

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
	orders.POST("/admin/affiliate-reversal", middleware.AdminAuth(), controller.ReverseTopUpAffiliateReward)

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

func registerUpstreamRoutes(api *gin.RouterGroup) {
	up := api.Group("/admin/upstreams", middleware.AdminAuth(), middleware.DisableCache())
	handlePermissionRoute(up, http.MethodGet, "", authz.UpstreamRead, controller.ListManagedUpstreams)
	handlePermissionRoute(up, http.MethodGet, "/bindings", authz.UpstreamRead, controller.ListUpstreamBindingsForChannels)
	handlePermissionRoute(up, http.MethodPost, "/probe", authz.UpstreamSensitiveWrite, controller.ProbeUpstream)
	handlePermissionRoute(up, http.MethodPost, "/auth", authz.UpstreamSensitiveWrite, controller.StartUpstreamAuth)
	handlePermissionRoute(up, http.MethodPost, "/auth/verify", authz.UpstreamSensitiveWrite, controller.CompleteUpstreamAuth)
	handlePermissionRoute(up, http.MethodGet, "/:id", authz.UpstreamRead, controller.GetManagedUpstream)
	handlePermissionRoute(up, http.MethodPatch, "/:id", authz.UpstreamWrite, controller.EditManagedUpstream)
	handlePermissionRoute(up, http.MethodPatch, "/:id/endpoint", authz.UpstreamSensitiveWrite, controller.EditManagedEndpoint)
	handlePermissionRoute(up, http.MethodDelete, "/:id", authz.UpstreamSensitiveWrite, controller.DeleteManagedUpstream)
	handlePermissionRoute(up, http.MethodPost, "/:id/sync", authz.UpstreamOperate, controller.SyncManagedUpstream)
	handlePermissionRoute(up, http.MethodGet, "/:id/keys", authz.UpstreamRead, controller.ListManagedKeys)
	handlePermissionRoute(up, http.MethodPost, "/:id/keys", authz.UpstreamSensitiveWrite, controller.CreateManagedKey)
	handlePermissionRoute(up, http.MethodPatch, "/:id/keys/:key_id", authz.UpstreamWrite, controller.EditManagedKey)
	handlePermissionRoute(up, http.MethodPatch, "/:id/keys/:key_id/status", authz.UpstreamWrite, controller.ToggleManagedKey)
	handlePermissionRoute(up, http.MethodDelete, "/:id/keys/:key_id", authz.UpstreamSensitiveWrite, controller.DeleteManagedKey)
	handlePermissionRoute(up, http.MethodGet, "/:id/bindings", authz.UpstreamRead, controller.ListManagedBindings)
	handlePermissionRoute(up, http.MethodPost, "/:id/keys/:key_id/bind", authz.UpstreamWrite, controller.RequireUpstreamChannelWrite, controller.BindManagedChannel)
	handlePermissionRoute(up, http.MethodPost, "/:id/keys/:key_id/channel", authz.UpstreamWrite, controller.RequireUpstreamChannelWrite, controller.CreateManagedChannel)
	handlePermissionRoute(up, http.MethodPost, "/:id/channels", authz.UpstreamSensitiveWrite, controller.RequireUpstreamChannelWrite, controller.CreateManagedChannel)
	handlePermissionRoute(up, http.MethodPatch, "/:id/bindings/:channel_id", authz.UpstreamWrite, controller.RequireUpstreamChannelWrite, controller.EditManagedBinding)
	handlePermissionRoute(up, http.MethodDelete, "/:id/bindings/:channel_id", authz.UpstreamWrite, controller.RequireUpstreamChannelWrite, controller.DeleteManagedBinding)
}

func registerAnnouncementRoutes(api *gin.RouterGroup) {
	user := api.Group("/announcements", middleware.UserAuth(), middleware.DisableCache())
	handleSessionRoute(user, http.MethodGet, "", controller.ListUserAnnouncements)
	handleSessionRoute(user, http.MethodPost, "/read-all", controller.MarkAllUserAnnouncementsRead)
	handleSessionRoute(user, http.MethodPost, "/:id/read", controller.MarkUserAnnouncementRead)

	admin := api.Group("/admin/announcements", middleware.AdminAuth(), middleware.DisableCache())
	handlePermissionRoute(admin, http.MethodGet, "", authz.AnnouncementRead, controller.ListAdminAnnouncements)
	handlePermissionRoute(admin, http.MethodPost, "", authz.AnnouncementWrite, controller.CreateAdminAnnouncement)
	handlePermissionRoute(admin, http.MethodGet, "/:id", authz.AnnouncementRead, controller.GetAdminAnnouncement)
	handlePermissionRoute(admin, http.MethodPut, "/:id", authz.AnnouncementWrite, controller.UpdateAdminAnnouncement)
	handlePermissionRoute(admin, http.MethodDelete, "/:id", authz.AnnouncementWrite, controller.DeleteAdminAnnouncement)
	handlePermissionRoute(admin, http.MethodGet, "/:id/read-status", authz.AnnouncementRead, controller.ListAdminAnnouncementReadStatus)
}
