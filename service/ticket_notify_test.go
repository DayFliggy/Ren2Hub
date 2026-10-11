package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/dto"
	serviceauthz "github.com/QuantumNous/new-api/service/authz"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Ticket notices reach each recipient in the language that recipient saved,
// English when none is saved; the ticket title stays as the user wrote it.
func TestTicketNoticesFollowTheRecipientLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	truncate(t)
	originalClient := httpClient
	httpClient = &http.Client{}
	fetch := system_setting.GetFetchSetting()
	originalFetch := *fetch
	originalWorker := system_setting.WorkerUrl
	fetch.EnableSSRFProtection = false
	system_setting.WorkerUrl = ""
	originalLimit := constant.NotifyLimitCount
	constant.NotifyLimitCount = 10
	t.Cleanup(func() {
		httpClient = originalClient
		*fetch = originalFetch
		system_setting.WorkerUrl = originalWorker
		constant.NotifyLimitCount = originalLimit
	})

	var mu sync.Mutex
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var payload WebhookPayload
		require.NoError(t, common.Unmarshal(body, &payload))
		mu.Lock()
		received = append(received, r.URL.Path+" "+payload.Title+" | "+payload.Content)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	users := map[string]*model.User{}
	for _, recipient := range []struct {
		id   int
		name string
		lang string
		role int
	}{
		{91, "root", "zh-TW", common.RoleRootUser},
		{92, "user", "zh-CN", common.RoleCommonUser},
		{93, "agent", "", common.RoleAdminUser},
	} {
		user := &model.User{Id: recipient.id, Username: "ticket-" + recipient.name, AffCode: "ticket-" + recipient.name, Role: recipient.role, Status: common.UserStatusEnabled}
		user.SetSetting(dto.UserSetting{Language: recipient.lang, NotifyType: dto.NotifyTypeWebhook, WebhookUrl: server.URL + "/" + recipient.name})
		require.NoError(t, model.DB.Create(user).Error)
		users[recipient.name] = user
	}

	ticket := model.Ticket{ID: 7, UserID: 92, Title: "Refund <request>", Category: "billing"}
	notifyRootAboutTicketReply(ticket)
	notifyTicketUser(ticket.UserID, ticket.ID, i18n.MsgTicketNotifySupportReplied, ticket.Title)
	notifyLoadedTicketUser(users["agent"], ticket.ID, i18n.MsgTicketNotifyAssigned, ticket.Title)

	sort.Strings(received)
	assert.Equal(t, []string{
		"/agent Ticket assigned to you (#7) | Refund <request>",
		"/root 工單 #7 等待回覆 | 使用者在未指派負責人的工單中追加了訊息：Refund <request>",
		"/user 客服已回复您的工单（#7） | Refund <request>",
	}, received)
}

func TestRenderEmailNotifyContentEscapesTicketText(t *testing.T) {
	content := renderEmailNotifyContent(dto.NewNotify(
		dto.NotifyTypeTicketUpdate,
		"Ticket update",
		`<a href="https://example.invalid">open</a> & review`,
		nil,
	))

	assert.Equal(t, `&lt;a href=&#34;https://example.invalid&#34;&gt;open&lt;/a&gt; &amp; review`, content)
}

func TestRenderEmailNotifyContentPreservesTrustedEmailMarkup(t *testing.T) {
	content := renderEmailNotifyContent(dto.NewNotify(
		dto.NotifyTypeQuotaExceed,
		"Quota update",
		"Remaining: {{value}}<br/>",
		[]interface{}{10},
	))

	assert.Equal(t, "Remaining: 10<br/>", content)
}

func TestGetEligibleTicketAgentRechecksCurrentAuthorization(t *testing.T) {
	previousDB := model.DB
	previousMaster := common.IsMasterNode
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.CasbinRule{}, &model.AuthzRole{}))
	model.DB = db
	common.IsMasterNode = true
	require.NoError(t, serviceauthz.Init(db))
	t.Cleanup(func() {
		model.DB = previousDB
		common.IsMasterNode = previousMaster
	})

	agent := model.User{
		Id:       42,
		Username: "ticket-agent",
		Password: "disabled-test-password",
		Role:     common.RoleAdminUser,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(&agent).Error)

	eligible, ok := getEligibleTicketAgent(agent.Id)
	require.True(t, ok)
	assert.Equal(t, agent.Id, eligible.Id)

	require.NoError(t, db.Model(&agent).Update("status", common.UserStatusDisabled).Error)
	_, ok = getEligibleTicketAgent(agent.Id)
	assert.False(t, ok)

	require.NoError(t, db.Model(&agent).Updates(map[string]interface{}{
		"status": common.UserStatusEnabled,
		"role":   common.RoleCommonUser,
	}).Error)
	_, ok = getEligibleTicketAgent(agent.Id)
	assert.False(t, ok)

	require.NoError(t, db.Model(&agent).Updates(map[string]interface{}{
		"status": common.UserStatusEnabled,
		"role":   common.RoleAdminUser,
	}).Error)
	require.NoError(t, serviceauthz.SetUserPermissions(agent.Id, serviceauthz.PermissionsMap{
		serviceauthz.ResourceTicket: {
			serviceauthz.ActionReply: false,
		},
	}))
	_, ok = getEligibleTicketAgent(agent.Id)
	assert.False(t, ok)
}
