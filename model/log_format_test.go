package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func TestFormatUserLogsProjectsFrozenBillingEvidence(t *testing.T) {
	logs := []*Log{{Other: `{"price_scope":"channel_model","price_channel_id":9,"price_version_hash":"` + strings.Repeat("b", 64) + `","price_version_id":42,"quote_hash":"` + strings.Repeat("a", 64) + `","billing_mode":"expression","upstream_model_name":"upstream-model","billing_usage":{"input_tokens":9,"source":"provider","actual_facts":{"secret":"hidden"},"headers":{"authorization":"hidden"}},"charge_result":{"calculation_version":"billing-v3","final_quota":"9007199254740993","base_usd":"1","adjustments":[{"kind":"channel_multiplier","multiplier":"1","base_usd":"1","amount_usd":"0","result_usd":"1"}],"total_usd":"1","lines":[],"unknown":{"secret":"hidden"},"trace":{"cost":1,"matched_tier":"standard","request_rules":[{"cond":"param(\"secret\")","multiplier":"2","matched":true}]}},"unknown":"hidden"}`}}
	formatUserLogs(logs, 0)
	require.NotContains(t, logs[0].Other, "hidden")
	require.NotContains(t, logs[0].Other, "param(")
	require.NotContains(t, logs[0].Other, "actual_facts")
	require.Contains(t, logs[0].Other, `"final_quota":"9007199254740993"`)
	require.Contains(t, logs[0].Other, `"input_tokens":9`)
	require.Contains(t, logs[0].Other, `"price_scope":"channel_model"`)
	require.Contains(t, logs[0].Other, `"price_channel_id":9`)
	require.Contains(t, logs[0].Other, `"price_version_hash":"`+strings.Repeat("b", 64)+`"`)
	require.NotContains(t, logs[0].Other, `"cost"`)
	require.Contains(t, logs[0].Other, `"upstream_model_name":"upstream-model"`)
	formatUserLogs([]*Log{{Other: "invalid"}}, 0)
}

// TestFormatUserLogsStripsQuotaSaturation verifies the admin-only quota
// saturation marker (nested under other.admin_info) is removed for non-admin
// log views, since formatUserLogs strips the whole admin_info object.
func TestFormatUserLogsStripsQuotaSaturation(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"model_price":      0.004,
		"price_version_id": 42,
		"admin_info": map[string]interface{}{
			"quota_saturation": map[string]interface{}{
				"op":      "QuotaFromDecimal",
				"kind":    "overflow",
				"clamped": common.MaxQuota,
			},
		},
	})
	logs := []*Log{{Other: other}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	_, hasAdminInfo := parsed["admin_info"]
	require.False(t, hasAdminInfo, "admin_info (and nested quota_saturation) must be stripped for non-admin views")
	// Non-admin billing fields remain visible.
	require.Contains(t, parsed, "price_version_id")
	require.NotContains(t, parsed, "model_price")
}

func TestFormatUserLogsKeepsOnlySafeMetadata(t *testing.T) {
	other := common.MapToJsonStr(map[string]interface{}{
		"reasoning_effort": "high",
		"request_path":     "/v1/chat/completions",
	})
	logs := []*Log{{Other: other, ChannelName: "Visible channel"}}

	formatUserLogs(logs, 0)

	parsed, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	require.Equal(t, "high", parsed["reasoning_effort"])
	require.NotContains(t, parsed, "request_path")
	require.Equal(t, "Visible channel", logs[0].ChannelName)
}
