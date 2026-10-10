package controller

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelStatusSummaryRedactsBothAdminRepresentations(t *testing.T) {
	for _, raw := range []string{
		`Quota exceeded; Authorization: Bearer private-bearer; api_key=private-key; cookie=session-private; account person@example.test https://user:password@example.test/path?token=private-query`,
		`{"error":{"message":"Quota exceeded for person@example.test; token: sk-private-key","request":{"authorization":"private-bearer","body":"private-prompt"}},"credentials":"private-secret"}`,
		`Quota exceeded; response: {"key":"private-key","prompt":"private-prompt"}`,
	} {
		t.Run(raw[:14], func(t *testing.T) {
			channel := &model.Channel{Id: 1, Status: common.ChannelStatusAutoDisabled}
			channel.SetOtherInfo(map[string]any{"status_reason": raw, "status_time": int64(1790884800), "unrelated": true})
			original := channel.OtherInfo
			legacy, modern := buildLegacyChannelDTO(channel), buildNextAdminChannelDTO(channel)
			assert.Equal(t, legacy.StatusReason, modern.StatusReason)
			assert.Contains(t, modern.StatusReason, "Quota exceeded")
			assert.Equal(t, int64(1790884800), modern.StatusTime)
			for _, projection := range []any{legacy, modern} {
				encoded, err := common.Marshal(projection)
				require.NoError(t, err)
				for _, secret := range []string{"private-bearer", "private-key", "session-private", "person@", "private-query", "private-prompt", "private-secret", "password@"} {
					assert.NotContains(t, string(encoded), secret)
				}
			}
			assert.Equal(t, original, channel.OtherInfo, "viewing the reason must not mutate channel facts")
		})
	}
}

func TestChannelStatusSummaryBoundsAndStatus(t *testing.T) {
	reason := sanitizeChannelStatusReason("额度不足\n\x00\u202e" + strings.Repeat("请稍后重试", 150))
	assert.True(t, utf8.ValidString(reason))
	assert.Equal(t, channelStatusReasonLimit, utf8.RuneCountInString(reason))
	assert.NotContains(t, reason, "\n")
	assert.NotContains(t, reason, "\u202e")
	for _, info := range []string{`invalid private-key`, `{"status_reason":{},"status_time":1}`, `{"status_reason":"old","status_time":1.2}`} {
		channel := &model.Channel{Status: common.ChannelStatusAutoDisabled, OtherInfo: info}
		reason, when := channelStatusSummary(channel)
		assert.Empty(t, reason)
		assert.Zero(t, when)
	}
	for _, status := range []int{common.ChannelStatusEnabled, common.ChannelStatusManuallyDisabled} {
		channel := &model.Channel{Status: status, OtherInfo: `{"status_reason":"stale reason","status_time":1}`}
		reason, when := channelStatusSummary(channel)
		assert.Empty(t, reason)
		assert.Zero(t, when)
	}
	assert.Equal(t, "The upstream error response has no displayable message; its body is hidden", sanitizeChannelStatusReason(`{"secret":"private-key"}`))
	assert.Equal(t, "The upstream error response could not be parsed; its body is hidden", sanitizeChannelStatusReason(`{"secret":`))
	assert.Equal(t, "rate limited [upstream response body hidden]", sanitizeChannelStatusReason(`rate limited {"detail":"private"}`))
	assert.Equal(t, "invalid key [hidden]", sanitizeChannelStatusReason(`invalid key sk-private0123`))
	for _, raw := range []string{`invalid token: private-value`, `invalid "api_key": "private-value"`, `invalid API key=private-value`, `invalid 'password': 'private-value'`} {
		assert.NotContains(t, sanitizeChannelStatusReason(raw), "private-value")
	}
}
