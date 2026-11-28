package helper

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const upstreamKeepAliveEvent = `{"type":"response.output_text.delta","item_id":"SSE-Keep-Alive","output_index":0,"content_index":0,"delta":"","SSE-Keep-Alive":true}`

func TestIsUpstreamKeepAliveEvent(t *testing.T) {
	for _, tc := range []struct {
		data string
		want bool
	}{
		{upstreamKeepAliveEvent, true},
		{`{"type":"response.output_text.delta","item_id":"SSE-Keep-Alive","delta":""}`, true},
		{`{"type":"response.output_text.delta","item_id":"msg_1","delta":"Hel","sequence_number":5}`, false},
		{`{"type":"response.output_text.delta","item_id":"msg_1","delta":""}`, false},
		{`{"type":"response.reasoning_summary_text.delta","item_id":"SSE-Keep-Alive","delta":"thinking"}`, false},
		{`{"type":"response.output_item.added","item":{"id":"SSE-Keep-Alive","type":"reasoning","encrypted_content":"abc"}}`, false},
		{`{"type":"response.created","response":{"id":"resp_1"},"SSE-Keep-Alive":true}`, false},
		{`not json SSE-Keep-Alive`, false},
	} {
		assert.Equal(t, tc.want, IsUpstreamKeepAliveEvent(tc.data), tc.data)
	}
}

func TestStreamScannerHandler_UpstreamKeepAlive(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	setting := operation_setting.GetGeneralSetting()
	oldDrop := setting.DropUpstreamKeepAliveEnabled
	t.Cleanup(func() { setting.DropUpstreamKeepAliveEnabled = oldDrop })

	created := `{"type":"response.created","response":{"id":"resp_1"}}`
	largeDelta := `{"type":"response.output_text.delta","item_id":"msg_1","delta":"` + strings.Repeat("x", 10*1024) + `"}`
	for _, tc := range []struct {
		name          string
		drop          bool
		body          string
		want          []string
		firstResponse bool
	}{
		{"dropped before first response", true, "data: " + upstreamKeepAliveEvent + "\n\ndata: " + created + "\n\n", []string{created}, true},
		{"only keep-alive", true, "data: " + upstreamKeepAliveEvent + "\n\n", nil, false},
		{"only keep-alive without trailing newline", true, "data: " + upstreamKeepAliveEvent, nil, false},
		{"large line passes through intact after keep-alive", true, "data: " + upstreamKeepAliveEvent + "\n\ndata: " + largeDelta + "\n\n", []string{largeDelta}, true},
		{"filter disabled", false, "data: " + upstreamKeepAliveEvent + "\n\ndata: " + created + "\n\n", []string{upstreamKeepAliveEvent, created}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			setting.DropUpstreamKeepAliveEnabled = tc.drop
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{Body: io.NopCloser(strings.NewReader(tc.body))}
			info := relaycommon.GenRelayInfoOpenAI(c, &dto.GeneralOpenAIRequest{Model: "gpt-test"})
			info.DisablePing = true
			var got []string
			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
				got = append(got, data)
			})
			assert.Equal(t, tc.want, got)
			assert.Equal(t, len(tc.want), info.ReceivedResponseCount)
			assert.Equal(t, tc.firstResponse, info.HasSendResponse())
		})
	}
}

func TestStreamScannerHandler_ResponseFailedLogsFailureSummary(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	var logBuf bytes.Buffer
	common.LogWriterMu.Lock()
	prevInfo, prevErr := gin.DefaultWriter, gin.DefaultErrorWriter
	gin.DefaultWriter = &logBuf
	gin.DefaultErrorWriter = &logBuf
	common.LogWriterMu.Unlock()
	t.Cleanup(func() {
		common.LogWriterMu.Lock()
		gin.DefaultWriter = prevInfo
		gin.DefaultErrorWriter = prevErr
		common.LogWriterMu.Unlock()
	})

	body := "data: {\"type\":\"response.failed\"}\n\ndata: [DONE]\n\n"
	c, resp, info := setupStreamTest(t, strings.NewReader(body))
	info.DisablePing = true
	StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
		info.StreamStatus.MarkFailed("server_error", "api_error", 0)
		info.StreamStatus.RecordFailureMessage("upstream failed for api_key:AIzaSyAAAaUooTUni8AdaOkSRMda30n_Q4vrV70")
	})

	logged := logBuf.String()
	assert.Contains(t, logged, "stream ended:")
	assert.Contains(t, logged, "response=failed")
	assert.Contains(t, logged, `code="server_error"`)
	assert.Contains(t, logged, `type="api_error"`)
	assert.Contains(t, logged, `message=`)
	assert.NotContains(t, logged, "AIzaSyAAAaUooTUni8AdaOkSRMda30n_Q4vrV70")
}
