package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 本地钩子只接管 sub2api/axonhub 草稿；带上游的请求必须原样交给原生流程。
// 用例都在发起网络请求之前结束，不访问任何外部来源。
func TestFetchUpstreamRatiosRoutesFixedSourcesAndNativeUpstreams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, body, contentType string
		status                  int
		message                 string
	}{
		// 原生预设覆盖的来源不再由本地解析，且没有 Content-Type 也不能绕过。
		{"native preset source rejected locally", `{"source_ids":["basellm"]}`, "", http.StatusBadRequest, "unknown price source: basellm"},
		{"fixed and channel sources are exclusive", `{"source_ids":["sub2api"],"channel_ids":[1]}`, "application/json", http.StatusBadRequest, "source_ids cannot be combined with channel sources"},
		// 原生流程能再次绑定请求体，才会走到上游过滤并报告没有可用上游。
		{"native flow rebinds restored body", `{"upstreams":[{"name":"x","base_url":"ftp://example.invalid"}]}`, "application/json", http.StatusOK, "No valid upstream channel"},
		// include_unchanged 属于原生请求字段，不改变本地钩子的分派。
		{"include_unchanged keeps fixed source dispatch", `{"source_ids":["basellm"],"include_unchanged":true}`, "application/json", http.StatusBadRequest, "unknown price source: basellm"},
		{"include_unchanged reaches native flow", `{"upstreams":[{"name":"x","base_url":"ftp://example.invalid"}],"include_unchanged":true}`, "application/json", http.StatusOK, "No valid upstream channel"},
		{"malformed body", `{`, "application/json", http.StatusBadRequest, "Invalid request parameter format"},
		{"empty body", ``, "application/json", http.StatusBadRequest, "Invalid request parameter format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/ratio_sync/fetch", strings.NewReader(tc.body))
			if tc.contentType != "" {
				c.Request.Header.Set("Content-Type", tc.contentType)
			}
			defer common.CleanupBodyStorage(c)
			FetchUpstreamRatios(c)
			require.Equal(t, tc.status, recorder.Code)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.False(t, response.Success)
			require.Equal(t, tc.message, response.Message)
		})
	}
}
