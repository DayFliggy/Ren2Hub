//go:build known_upstream_restored

package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/stretchr/testify/require"
)

func TestRestoredNativeControlConsoleNormalizationMatrix(t *testing.T) {
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
		for index, input := range []string{"", " \t\n"} {
			label := "empty"
			if index == 1 {
				label = "whitespace"
			}
			t.Run(key+"/"+label, func(t *testing.T) {
				require.NoError(t, updateOptionMap(fullKey, input))
				if common.OptionMap[fullKey] != "[]" {
					t.Error("CONTROL_CONSOLE_OPTION_MAP: blank list was not normalized")
				}
				if config.GlobalConfig.ExportAllConfigs()[fullKey] != "[]" {
					t.Error("CONTROL_CONSOLE_EXPORTED_CONFIG: blank list was not normalized")
				}
			})
		}
	}
}
