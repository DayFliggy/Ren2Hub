package console_setting

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Validation errors reach the web console as message keys with the 1-based
// item index, and each list reports its own key for the shared URL and
// content checks.
func TestValidateConsoleSettingsReturnsMessages(t *testing.T) {
	cases := []struct {
		name        string
		settingType string
		value       string
		key         string
		params      map[string]any
	}{
		{
			name:        "api info url format",
			settingType: "ApiInfo",
			value:       `[{"url":"https://a.example","route":"r","description":"d","color":"blue"},{"url":"ftp://bad","route":"r","description":"d","color":"blue"}]`,
			key:         "API info #{{index}} has an invalid URL format",
			params:      map[string]any{"index": 2},
		},
		{
			name:        "api info unsafe route",
			settingType: "ApiInfo",
			value:       `[{"url":"https://a.example","route":"<script>","description":"d","color":"blue"}]`,
			key:         "API info #{{index}} contains disallowed content",
			params:      map[string]any{"index": 1},
		},
		{
			name:        "group url format",
			settingType: "UptimeKumaGroups",
			value:       `[{"categoryName":"c","url":"bad","slug":"s"}]`,
			key:         "Group #{{index}} has an invalid URL format",
			params:      map[string]any{"index": 1},
		},
		{
			name:        "group unsafe category",
			settingType: "UptimeKumaGroups",
			value:       `[{"categoryName":"javascript:x","url":"https://a.example","slug":"s"}]`,
			key:         "Group #{{index}} contains disallowed content",
			params:      map[string]any{"index": 1},
		},
		{
			name:        "unknown type",
			settingType: "Other",
			value:       `[]`,
			key:         "Unknown setting type: {{type}}",
			params:      map[string]any{"type": "Other"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConsoleSettings(tc.value, tc.settingType)
			var message *common.Message
			require.True(t, errors.As(err, &message), "error %v is not a *common.Message", err)
			assert.Equal(t, tc.key, message.Key)
			assert.Equal(t, tc.params, message.Params)
		})
	}
}

func TestValidateConsoleSettingsAcceptsValidLists(t *testing.T) {
	require.NoError(t, ValidateConsoleSettings(`[{"url":"https://a.example","route":"r","description":"d","color":"blue"}]`, "ApiInfo"))
	require.NoError(t, ValidateConsoleSettings(`[{"categoryName":"c","url":"https://a.example","slug":"s-1"}]`, "UptimeKumaGroups"))
}
