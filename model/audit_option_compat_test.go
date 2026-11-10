package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyConsoleOptionsNormalizeWithoutMaskingInvalidJSON(t *testing.T) {
	previous := *console_setting.GetConsoleSetting()
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		*console_setting.GetConsoleSetting() = previous
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})
	for _, key := range []string{"api_info", "announcements", "faq", "uptime_kuma_groups"} {
		fullKey := "console_setting." + key
		for _, input := range []string{"", " \t\n"} {
			require.NoError(t, updateOptionMap(fullKey, input))
			assert.Equal(t, "[]", common.OptionMap[fullKey])
			assert.Equal(t, "[]", config.GlobalConfig.ExportAllConfigs()[fullKey])
		}
		require.NoError(t, updateOptionMap(fullKey, "{broken"))
		assert.Equal(t, "{broken", common.OptionMap[fullKey], "invalid nonempty JSON must remain visible for repair")
	}
	// Removed fields are rejected instead of being stored and ignored.
	for key, value := range map[string]string{
		"quota_setting.trust_quota_usd":   "20",
		"payment_setting.amount_discount": `{"100":0.9}`,
	} {
		common.OptionMap[key] = "stale"
		require.NoError(t, updateOptionMap(key, value))
		assert.NotContains(t, common.OptionMap, key)
		assert.NotContains(t, config.GlobalConfig.ExportAllConfigs(), key)
		require.Error(t, validateOptionValue(key, value))
	}
}
