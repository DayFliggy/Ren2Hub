package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ops monitor and task plugin rejections reach the web console as message_key,
// with dynamic validation detail passed as the {{error}} param.
func TestAdminConsoleRejectionsCarryMessageKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/ops/requests", GetOpsRequests)
	router.POST("/api/plugin/task/:key/dry-run", DryRunTaskPlugin)
	router.PUT("/api/plugin/task/marketplace/sources", UpdateTaskPluginMarketplaceSources)
	for _, tc := range []struct {
		name, method, path, body, key string
		params                        []string
	}{
		{"ops hours out of range", http.MethodGet, "/api/ops/requests?hours=0", "", "hours must be between {{min}} and {{max}}", []string{"max", "min"}},
		{"plugin dry run bind error", http.MethodPost, "/api/plugin/task/demo/dry-run", "{", "Invalid request parameters: {{error}}", []string{"error"}},
		{"marketplace sources bind error", http.MethodPut, "/api/plugin/task/marketplace/sources", `{"name":1}`, "Invalid request parameters: {{error}}", []string{"error"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			var body struct {
				Success       bool           `json:"success"`
				Message       string         `json:"message"`
				MessageKey    string         `json:"message_key"`
				MessageParams map[string]any `json:"message_params"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
			assert.False(t, body.Success)
			assert.Equal(t, tc.key, body.MessageKey)
			params := make([]string, 0, len(body.MessageParams))
			for name := range body.MessageParams {
				params = append(params, name)
			}
			assert.ElementsMatch(t, tc.params, params)
			assert.NotContains(t, body.Message, "{{")
		})
	}
}
