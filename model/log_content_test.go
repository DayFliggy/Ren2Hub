package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Log content is stored as content_parts (English source keys and params) for
// the web console and as English text in the content column. The owner's view
// keeps content_parts while it strips admin-only metadata; the admin view
// keeps both.
func TestLogViewsKeepStructuredContent(t *testing.T) {
	truncateTables(t)
	user := User{Username: "content-owner", AffCode: "content-owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	RecordTopupLog(user.Id, common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": "$1.00", "amount": "7.30"}), "203.0.113.9", "alipay", PaymentProviderEpay)
	RecordLog(user.Id, LogTypeTopup, common.NewMessage("Topped up {{quota}} with a redemption code, code ID {{id}}", map[string]any{"quota": "$2.00", "id": 5}))

	parts := map[string]any{
		"Online top-up succeeded, amount added: $1.00, payment amount: 7.30": []any{map[string]any{
			"key":    "Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}",
			"params": map[string]any{"quota": "$1.00", "amount": "7.30"},
		}},
		"Topped up $2.00 with a redemption code, code ID 5": []any{map[string]any{
			"key":    "Topped up {{quota}} with a redemption code, code ID {{id}}",
			"params": map[string]any{"quota": "$2.00", "id": float64(5)},
		}},
	}

	owned, total, err := GetUserLogs(user.Id, LogTypeTopup, 0, 0, "", "", 0, 10, "", "", "", "")
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	for _, log := range owned {
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		assert.Equal(t, parts[log.Content], other["content_parts"], log.Content)
		assert.NotContains(t, other, "admin_info", log.Content)
	}

	all, _, err := GetAllLogs(LogTypeTopup, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, all, 2)
	for _, log := range all {
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		assert.Equal(t, parts[log.Content], other["content_parts"], log.Content)
	}
}
