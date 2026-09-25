package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/console_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetStatusPreservesConfiguredAPIAddresses(t *testing.T) {
	settings := console_setting.GetConsoleSetting()
	previous, previousAddress := *settings, system_setting.ServerAddress
	t.Cleanup(func() { *settings = previous; system_setting.ServerAddress = previousAddress })
	system_setting.ServerAddress = "https://fallback.example.test/gateway/"
	for _, test := range []struct {
		name    string
		enabled bool
		info    string
		count   int
	}{
		{"configured", true, `[{"url":"https://api.example.test/gateway/v1/","route":"custom","description":"configured endpoint"}]`, 1},
		{"disabled", false, `[{"url":"https://api.example.test/gateway/v1/"}]`, 0},
		{"empty", true, `[]`, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			settings.ApiInfoEnabled, settings.ApiInfo = test.enabled, test.info
			router := gin.New()
			router.GET("/api/status", GetStatus)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/status", nil))
			require.Equal(t, http.StatusOK, recorder.Code)
			var envelope struct {
				Success bool           `json:"success"`
				Data    map[string]any `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &envelope))
			require.True(t, envelope.Success)
			assert.Equal(t, system_setting.ServerAddress, envelope.Data["server_address"])
			assert.Equal(t, test.enabled, envelope.Data["api_info_enabled"])
			if !test.enabled {
				assert.NotContains(t, envelope.Data, "api_info")
				return
			}
			entries, ok := envelope.Data["api_info"].([]any)
			require.True(t, ok)
			require.Len(t, entries, test.count)
			if test.count > 0 {
				assert.Equal(t, map[string]any{"url": "https://api.example.test/gateway/v1/", "route": "custom", "description": "configured endpoint"}, entries[0])
			}
		})
	}
}
