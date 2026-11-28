package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiResponsesHandlersFillMissingTextFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() {
		constant.StreamingTimeout = oldTimeout
	})
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			DisablePing: true,
			ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-test"},
		}
	}

	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{"format dropped", `{"verbosity":"medium"}`, `"text":{"verbosity":"medium","format":{"type":"text"}}`},
		{"client schema kept", `{"format":{"type":"json_schema","name":"x"}}`, `"text":{"format":{"type":"json_schema","name":"x"}}`},
		{"non-object text kept", `"plain"`, `"text":"plain"`},
	} {
		t.Run(tc.name+" stream", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set(common.RequestIdKey, "text-format-stream")
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body: io.NopCloser(strings.NewReader(
					"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"text\":" + tc.text + "}}\n\n" +
						"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[],\"text\":" + tc.text + "}}\n\n" +
						"data: [DONE]\n\n",
				)),
				Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			}
			_, apiErr := OaiResponsesStreamHandler(c, newInfo(), resp)
			require.Nil(t, apiErr)
			assert.Equal(t, 2, strings.Count(w.Body.String(), tc.want))
		})
		t.Run(tc.name+" non-stream", func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","object":"response","status":"completed","output":[],"text":` + tc.text + `}`)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}
			_, apiErr := OaiResponsesHandler(c, newInfo(), resp)
			require.Nil(t, apiErr)
			assert.Contains(t, w.Body.String(), tc.want)
		})
	}
}
