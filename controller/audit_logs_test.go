package controller

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditKeywordSearchIsLiteralAndSkipsMetadata(t *testing.T) {
	user, pat := setupAccessTokenAudit(t)
	for _, entry := range []model.AuditLog{
		{ActorRole: common.RoleAdminUser, Category: model.AuditCategorySystem, Action: "user.checkin", Content: "Granted 100% reward", CreatedAt: 100},
		{ActorRole: common.RoleAdminUser, Category: model.AuditCategorySystem, Action: "user.checkin", Content: "Granted 1000 reward", CreatedAt: 101},
		{ActorRole: common.RoleRootUser, Category: model.AuditCategorySystem, Content: "Granted 100% as root", CreatedAt: 102},
		{ActorRole: common.RoleAdminUser, Category: model.AuditCategoryOperation, Action: "user_update", Content: "updated", CreatedAt: 103, Other: model.AuditOther{AdminInfo: &model.AuditAdminInfo{AdminUsername: "hidden-operator"}}},
	} {
		entry.UserId, entry.Username, entry.Success = user.Id, user.Username, true
		model.RecordAuditLog(nil, entry)
	}
	router := gin.New()
	router.Use(middleware.RequestId())
	router.GET("/api/audit/self", middleware.UserAuth(), GetAuditLogs)
	cases := []struct {
		query string
		total int
	}{
		{"category=system&keyword=" + url.QueryEscape("100%"), 1}, // literal %, root row hidden
		{"keyword=user_", 1},           // literal _, does not match user.checkin
		{"keyword=hidden-operator", 0}, // metadata is not searchable
		{"category=system", 2},
	}
	for _, tc := range cases {
		response := auditRequest(router, http.MethodGet, "/api/audit/self?"+tc.query, pat)
		var result struct {
			Success bool
			Data    struct{ Total int }
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result), tc.query)
		assert.True(t, result.Success, tc.query)
		assert.Equal(t, tc.total, result.Data.Total, tc.query)
	}
	tooLong := auditRequest(router, http.MethodGet, "/api/audit/self?keyword="+strings.Repeat("a", 129), pat)
	assert.Contains(t, tooLong.Body.String(), `"success":false`)
	// The limit counts characters, matching the input's maxlength.
	cjk := auditRequest(router, http.MethodGet, "/api/audit/self?keyword="+url.QueryEscape(strings.Repeat("审", 128)), pat)
	assert.Contains(t, cjk.Body.String(), `"success":true`)
	cjkTooLong := auditRequest(router, http.MethodGet, "/api/audit/self?keyword="+url.QueryEscape(strings.Repeat("审", 129)), pat)
	assert.Contains(t, cjkTooLong.Body.String(), `"success":false`)
}

func TestAuditReadStripsCredentialParams(t *testing.T) {
	user, pat := setupAccessTokenAudit(t)
	model.RecordAuditLog(nil, model.AuditLog{
		UserId: user.Id, Username: user.Username, ActorRole: user.Role, Category: model.AuditCategoryOperation,
		Action: "user.update", Content: "Updated", Success: true,
		Other: model.AuditOther{Op: &model.AuditOperation{Action: "user.update", Params: model.AuditFields{
			"id": 9007199254740993, "password": "leak", "nested": map[string]any{"client_secret": "leak", "keep": true},
			"list": []any{map[string]any{"access_token": "leak", "n": 1}}, "token_ref": "kept",
		}}},
	})
	router := gin.New()
	router.Use(middleware.RequestId())
	router.GET("/api/audit/self", middleware.UserAuth(), GetAuditLogs)
	response := auditRequest(router, http.MethodGet, "/api/audit/self?category=operation", pat)
	require.Equal(t, http.StatusOK, response.Code)
	body := response.Body.String()
	assert.NotContains(t, body, "leak")
	assert.Contains(t, body, `"id":9007199254740993`)
	assert.Contains(t, body, `"keep":true`)
	assert.Contains(t, body, `"n":1`)
	assert.Contains(t, body, `"token_ref":"kept"`)
}

func TestSystemRewardAuditBelongsToRecipient(t *testing.T) {
	admin, pat := setupAccessTokenAudit(t)
	member := &model.User{Username: "reward-member", Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "reward-member"}
	require.NoError(t, model.DB.Create(member).Error)
	router := gin.New()
	router.Use(middleware.RequestId())
	router.POST("/checkin", middleware.UserAuth(), func(c *gin.Context) {
		recordUserSystemAudit(c, c.GetInt("id"), "user.checkin", map[string]any{"quota": 25})
		c.Status(http.StatusOK)
	})
	require.Equal(t, http.StatusOK, auditRequest(router, http.MethodPost, "/checkin", pat).Code)
	// Registration rewards are granted outside a request context.
	model.RecordAccountSystemAuditLog(member.Id, "Granted inviter quota", "user.inviter_bonus", map[string]any{"quota": 7, "invited_user_id": 99})

	entries, total, err := model.GetAuditLogs(model.AuditLogFilter{Category: model.AuditCategorySystem}, 0, 10, common.RoleRootUser)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	byAction := map[string]*model.AuditLog{}
	for _, entry := range entries {
		byAction[entry.Action] = entry
	}
	checkin := byAction["user.checkin"]
	require.NotNil(t, checkin)
	assert.Equal(t, admin.Id, checkin.UserId)
	assert.Equal(t, common.RoleAdminUser, checkin.ActorRole)
	assert.Equal(t, "/checkin", checkin.Route)
	assert.Equal(t, "192.0.2.8", checkin.Ip)
	assert.Equal(t, "access_token", checkin.AuthMethod)
	assert.Equal(t, "Completed daily check-in and received 25 quota", checkin.Content)
	bonus := byAction["user.inviter_bonus"]
	require.NotNil(t, bonus)
	assert.Equal(t, member.Id, bonus.UserId)
	assert.Equal(t, member.Username, bonus.Username)
	assert.Equal(t, common.RoleCommonUser, bonus.ActorRole)
	params, err := common.Marshal(bonus.Other.Op.Params)
	require.NoError(t, err)
	assert.JSONEq(t, `{"quota":7,"invited_user_id":99}`, string(params))

	own, total, err := model.GetAuditLogs(model.AuditLogFilter{UserId: member.Id, SelfView: true}, 0, 10, common.RoleCommonUser)
	require.NoError(t, err)
	require.EqualValues(t, 1, total, "recipients must see their own reward events")
	assert.Equal(t, "user.inviter_bonus", own[0].Action)
	var legacyRows int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Where("type = ?", model.LogTypeSystem).Count(&legacyRows).Error)
	assert.Zero(t, legacyRows, "system events must no longer be written to purgeable usage logs")
}

func TestLegacyAuditBackfillIsIdempotentAndKeepsVisibility(t *testing.T) {
	admin, _ := setupAccessTokenAudit(t)
	member := &model.User{Username: "legacy-member", Password: "placeholder", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AffCode: "legacy-member"}
	require.NoError(t, model.DB.Create(member).Error)
	legacy := []model.Log{
		{Type: model.LogTypeLogin, UserId: member.Id, Username: member.Username, CreatedAt: 100, Content: "Logged in", Ip: "198.51.100.1", RequestId: "legacy-login",
			Other: `{"op":{"action":"login","params":{"method":"password"}},"login_method":"password","user_agent":"legacy-agent","user_role":1}`},
		// Upstream manage rows are owned by the target user.
		{Type: model.LogTypeManage, UserId: member.Id, Username: member.Username, CreatedAt: 101, Content: "Updated user", RequestId: "legacy-manage",
			Other: `{"op":{"action":"user.update","params":{"id":9007199254740993,"password":"leak","nested":{"api_key":"leak","keep":true},"list":[{"access_token":"leak","n":1}]}},"admin_info":{"admin_id":` + fmt.Sprint(admin.Id) + `,"admin_username":"audit-owner","admin_role":10,"auth_method":"session"},"audit_info":{"method":"PUT","route":"/api/user/","path":"/api/user/","status":200,"success":false}}`},
		{Type: model.LogTypeSystem, UserId: member.Id, Username: member.Username, CreatedAt: 102, Content: "Granted invitee quota", Other: `{"op":{"action":"user.invitee_bonus","params":{"quota":5}}}`},
		{Type: model.LogTypeManage, UserId: member.Id, Username: member.Username, CreatedAt: 103, Content: "Unknown operator", Other: `{"op":{"action":"user.manage"}}`},
		// user_role names the row owner (the target), never the operator.
		{Type: model.LogTypeManage, UserId: member.Id, Username: member.Username, CreatedAt: 106, Content: "Target role only", Other: `{"op":{"action":"user.manage"},"user_role":1}`},
		{Type: model.LogTypeLogin, UserId: member.Id, Username: member.Username, CreatedAt: 104, Content: "Broken metadata", Other: `{broken`},
		{Type: model.LogTypeConsume, UserId: member.Id, Username: member.Username, CreatedAt: 105, Content: "usage stays in logs"},
	}
	require.NoError(t, model.LOG_DB.Create(&legacy).Error)
	require.NoError(t, model.BackfillLegacyAuditLogs())
	// Simulate an interrupted run whose marker was not written.
	require.NoError(t, model.DB.Where("1 = 1").Delete(&model.Option{}).Error)
	require.NoError(t, model.BackfillLegacyAuditLogs())
	require.NoError(t, model.BackfillLegacyAuditLogs())

	entries, total, err := model.GetAuditLogs(model.AuditLogFilter{}, 0, 20, common.RoleRootUser)
	require.NoError(t, err)
	require.EqualValues(t, 6, total, "backfill must be idempotent and skip business logs")
	byContent := map[string]*model.AuditLog{}
	for _, entry := range entries {
		byContent[entry.Content] = entry
	}
	login := byContent["Logged in"]
	require.NotNil(t, login)
	assert.Equal(t, model.AuditCategoryLogin, login.Category)
	assert.Equal(t, common.RoleCommonUser, login.ActorRole)
	assert.Equal(t, "legacy-agent", login.UserAgent)
	assert.Equal(t, "legacy-login", login.RequestId)

	manage := byContent["Updated user"]
	require.NotNil(t, manage)
	assert.Equal(t, model.AuditCategoryOperation, manage.Category)
	assert.Equal(t, admin.Id, manage.UserId, "operation audits belong to the operator")
	assert.Equal(t, common.RoleAdminUser, manage.ActorRole)
	assert.False(t, manage.Success)
	assert.Equal(t, "PUT", manage.Method)
	params, err := common.Marshal(manage.Other.Op.Params)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":9007199254740993,"nested":{"keep":true},"list":[{"n":1}],"target_user_id":`+fmt.Sprint(member.Id)+`}`, string(params))

	system := byContent["Granted invitee quota"]
	require.NotNil(t, system)
	assert.Equal(t, model.AuditCategorySystem, system.Category)
	assert.Equal(t, common.RoleCommonUser, system.ActorRole, "old reward rows take the recipient role")
	assert.Zero(t, byContent["Unknown operator"].ActorRole, "operations without a recorded role stay root only")
	assert.Zero(t, byContent["Target role only"].ActorRole, "a target's role must not become the operator's role")
	rootInfo, err := common.Marshal(byContent["Broken metadata"].Other.RootInfo)
	require.NoError(t, err)
	assert.JSONEq(t, `{"legacy_other":"{broken"}`, string(rootInfo), "unparseable metadata is kept for root")

	own, total, err := model.GetAuditLogs(model.AuditLogFilter{UserId: member.Id, SelfView: true}, 0, 20, common.RoleCommonUser)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total, "members see their own login and reward rows, not root-only rows")
	for _, entry := range own {
		assert.Nil(t, entry.Other.RootInfo)
	}
	var sourceRows int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).Count(&sourceRows).Error)
	assert.EqualValues(t, len(legacy), sourceRows, "source rows are retained")
}
