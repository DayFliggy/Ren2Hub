package service

import (
	"fmt"
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
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Low-quota notices are rendered in the recipient's saved language and in
// English (DEFAULT_LANGUAGE unset) when none is saved. Bark and Gotify get
// plain text; email and webhook get the top-up link.
func TestQuotaNotifyContentFollowsTheSavedLanguage(t *testing.T) {
	require.NoError(t, i18n.Init())
	for _, tc := range []struct {
		lang       string
		notifyType string
		want       string
	}{
		{"zh-CN", dto.NotifyTypeBark, "您的额度即将用尽，剩余额度：$0.10，请及时充值"},
		{"zh-CN", dto.NotifyTypeGotify, "您的额度即将用尽，当前剩余额度为 $0.10，请及时充值。"},
		{"zh-TW", dto.NotifyTypeBark, "您的額度即將用盡，剩餘額度：$0.10，請及時儲值"},
		{"", dto.NotifyTypeBark, "Your quota is running low, remaining quota: $0.10. Please top up soon"},
		{"en", dto.NotifyTypeGotify, "Your quota is running low. Your remaining quota is $0.10. Please top up soon."},
	} {
		prompt := i18n.Translate(tc.lang, i18n.MsgNotifyQuotaLowTitle)
		content, values := quotaNotifyContent(tc.lang, tc.notifyType, prompt, "$0.10")
		assert.Equal(t, tc.want, content, "%s %s", tc.lang, tc.notifyType)
		assert.Equal(t, []any{prompt, "$0.10"}, values)
	}

	link := PaymentReturnURL("/wallet")
	content, values := quotaNotifyContent("zh-CN", dto.NotifyTypeEmail, "您的额度即将用尽", "$0.10")
	assert.Equal(t, "您的额度即将用尽，当前剩余额度为 $0.10，为了不影响您的使用，请及时充值。<br/>充值链接：<a href='"+link+"'>"+link+"</a>", content)
	assert.Equal(t, []any{"您的额度即将用尽", "$0.10", link, link}, values)
	content, _ = quotaNotifyContent("", dto.NotifyTypeWebhook, "Your quota is running low", "$0.10")
	assert.Equal(t, "Your quota is running low. Your remaining quota is $0.10. Top up soon to keep using the service.<br/>Top-up link: <a href='"+link+"'>"+link+"</a>", content)
}

// Channel notices go to the root user in the language the root user saved.
func TestRootUserLanguage(t *testing.T) {
	truncate(t)
	assert.Empty(t, RootUserLanguage())
	root := &model.User{Id: 71, Username: "root-language", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	root.SetSetting(dto.UserSetting{Language: "zh-TW"})
	require.NoError(t, model.DB.Create(root).Error)
	assert.Equal(t, "zh-TW", RootUserLanguage())
}

// Each administrator who enabled upstream model update notices receives it
// rendered in the language that administrator saved.
func TestNotifyUpstreamModelUpdateWatchersRendersPerRecipient(t *testing.T) {
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

	for _, admin := range []struct {
		id      int
		lang    string
		enabled bool
		role    int
	}{
		{81, "zh-CN", true, common.RoleAdminUser},
		{82, "", true, common.RoleRootUser},
		{83, "zh-TW", false, common.RoleAdminUser},
		{84, "zh-TW", true, common.RoleCommonUser},
	} {
		name := fmt.Sprintf("watcher-%d", admin.id)
		user := &model.User{Id: admin.id, Username: name, AffCode: name, Role: admin.role, Status: common.UserStatusEnabled}
		user.SetSetting(dto.UserSetting{
			Language:                         admin.lang,
			NotifyType:                       dto.NotifyTypeWebhook,
			WebhookUrl:                       server.URL + "/" + user.Username,
			UpstreamModelUpdateNotifyEnabled: admin.enabled,
		})
		require.NoError(t, model.DB.Create(user).Error)
	}

	NotifyUpstreamModelUpdateWatchers(func(lang string) (string, string) {
		return i18n.Translate(lang, i18n.MsgChannelUpstreamUpdateNotifySubject), "lang=" + lang
	})

	sort.Strings(received)
	assert.Equal(t, []string{
		"/watcher-81 上游模型巡检通知 | lang=zh-CN",
		"/watcher-82 Upstream model check notice | lang=",
	}, received)
}
