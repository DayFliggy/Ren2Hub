package controller

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFrontendContractAnnouncements(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Announcement{}, &model.AnnouncementRead{}))

	u1 := model.User{Id: 101, Username: "visual.root", DisplayName: "Visual Root", Email: "visual-root@ren2hub.dev", AffCode: "VROOT", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	u2 := model.User{Id: 102, Username: "member.demo", DisplayName: "Member Demo", Email: "member@ren2hub.dev", AffCode: "MDEMO", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&u1).Error)
	require.NoError(t, model.DB.Create(&u2).Error)

	startsAt := int64(1785000000)
	endsAt := int64(1893456000)
	a1 := model.Announcement{
		ID:         11,
		Title:      "平台路由与模型目录升级公告",
		Content:    "### 本次更新\n\n- 优化自动选渠与缓存复用统计\n- 新增控制台公告中心与已读追踪\n\n详情请查看 [状态页](https://next.renai.uno)。",
		Type:       model.AnnouncementTypeOngoing,
		Status:     model.AnnouncementStatusActive,
		NotifyMode: model.AnnouncementNotifyModeSilent,
		Pinned:     true,
		StartsAt:   &startsAt,
		EndsAt:     &endsAt,
		CreatedBy:  u1.Id,
		UpdatedBy:  u1.Id,
		CreatedAt:  1785120000,
		UpdatedAt:  1785120000,
	}
	a2 := model.Announcement{
		ID:         12,
		Title:      "余额结算维护通知",
		Content:    "维护窗口内不影响在途请求，完成后将自动恢复。",
		Type:       model.AnnouncementTypeWarning,
		Status:     model.AnnouncementStatusActive,
		NotifyMode: model.AnnouncementNotifyModePopup,
		Pinned:     false,
		CreatedBy:  u1.Id,
		UpdatedBy:  u1.Id,
		CreatedAt:  1785050000,
		UpdatedAt:  1785050000,
	}
	a3 := model.Announcement{
		ID:         13,
		Title:      "下季度计费调整草稿",
		Content:    "内部审阅草稿，暂不对普通用户展示。",
		Type:       model.AnnouncementTypeDefault,
		Status:     model.AnnouncementStatusDraft,
		NotifyMode: model.AnnouncementNotifyModeSilent,
		Pinned:     false,
		CreatedBy:  u1.Id,
		UpdatedBy:  u1.Id,
		CreatedAt:  1785000000,
		UpdatedAt:  1785000000,
	}
	for _, ann := range []*model.Announcement{&a1, &a2, &a3} {
		require.NoError(t, model.CreateAnnouncement(ann))
	}
	require.NoError(t, model.DB.Create(&model.AnnouncementRead{
		AnnouncementID: a1.ID,
		UserID:         u2.Id,
		ReadAt:         1785123600,
	}).Error)

	engine := contractEngine(u1.Id, common.RoleRootUser)
	engine.GET("/api/announcements", ListUserAnnouncements)
	engine.GET("/api/admin/announcements", ListAdminAnnouncements)
	engine.GET("/api/admin/announcements/:id", GetAdminAnnouncement)
	engine.GET("/api/admin/announcements/:id/read-status", ListAdminAnnouncementReadStatus)

	fixture := gin.H{}
	for _, tc := range []struct{ key, path string }{
		{"user_list", "/api/announcements"},
		{"admin_list", "/api/admin/announcements?page=1&page_size=20"},
		{"admin_detail", "/api/admin/announcements/11"},
		{"read_status", "/api/admin/announcements/11/read-status?page=1&page_size=20"},
	} {
		w := contractRequest(t, engine, "GET", tc.path, nil)
		require.Equal(t, 200, w.Code, tc.path+": "+w.Body.String())
		body := decodeContractBody(t, w)
		require.Equal(t, true, body["success"])
		fixture[tc.key] = body
	}
	frontendFixture(t, "announcements.get.json", fixture)
}

func TestAnnouncementControllerUTF8KeywordAndNullScheduleClear(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.User{}, &model.Announcement{}, &model.AnnouncementRead{}))

	u1 := model.User{Id: 101, Username: "admin.edge", DisplayName: "Admin Edge", Email: "edge@ren2hub.dev", AffCode: "EDGE1", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&u1).Error)

	startsAt := int64(1785000000)
	endsAt := int64(1893456000)
	ann := model.Announcement{
		ID:         21,
		Title:      "测试多字节搜索与空值清空",
		Content:    "内容",
		Type:       model.AnnouncementTypeDefault,
		Status:     model.AnnouncementStatusActive,
		NotifyMode: model.AnnouncementNotifyModeSilent,
		StartsAt:   &startsAt,
		EndsAt:     &endsAt,
	}
	require.NoError(t, model.CreateAnnouncement(&ann))

	// Truncating >200 multi-byte runes must remain valid UTF-8.
	longChinese := strings.Repeat("公", 250)
	truncated := truncateRunes(longChinese, 200)
	assert.True(t, utf8.ValidString(truncated))
	assert.Equal(t, 200, utf8.RuneCountInString(truncated))

	engine := contractEngine(u1.Id, common.RoleRootUser)
	engine.GET("/api/admin/announcements", ListAdminAnnouncements)
	engine.GET("/api/admin/announcements/:id/read-status", ListAdminAnnouncementReadStatus)
	engine.PUT("/api/admin/announcements/:id", UpdateAdminAnnouncement)

	// Long Chinese query param succeeds cleanly on both endpoints.
	q := url.QueryEscape(longChinese)
	wList := contractRequest(t, engine, http.MethodGet, "/api/admin/announcements?keyword="+q, nil)
	require.Equal(t, http.StatusOK, wList.Code, wList.Body.String())
	wReads := contractRequest(t, engine, http.MethodGet, "/api/admin/announcements/21/read-status?keyword="+q, nil)
	require.Equal(t, http.StatusOK, wReads.Code, wReads.Body.String())

	// Explicit JSON null clears starts_at while omitted ends_at stays intact.
	wNull := contractRequest(t, engine, http.MethodPut, "/api/admin/announcements/21", []byte(`{"starts_at":null}`))
	require.Equal(t, http.StatusOK, wNull.Code, wNull.Body.String())
	body := decodeContractBody(t, wNull)
	data := body["data"].(map[string]any)
	assert.Nil(t, data["starts_at"])
	assert.Equal(t, float64(endsAt), data["ends_at"])

	// Idempotent same-second no-op update returns 200 (never false 404).
	wNoop := contractRequest(t, engine, http.MethodPut, "/api/admin/announcements/21", []byte(`{"title":"测试多字节搜索与空值清空"}`))
	require.Equal(t, http.StatusOK, wNoop.Code, wNoop.Body.String())
}
