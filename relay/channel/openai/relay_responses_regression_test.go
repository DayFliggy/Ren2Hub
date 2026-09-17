package openai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const responsesDetailedUsage = `{"input_tokens":100,"output_tokens":20,"total_tokens":120,"input_tokens_details":{"cached_tokens":10,"cache_write_tokens":3,"text_tokens":90,"audio_tokens":2,"image_tokens":8},"output_tokens_details":{"text_tokens":11,"audio_tokens":4,"image_tokens":5,"reasoning_tokens":6}}`

func runResponsesRegressionStream(t *testing.T, events []string, writer func(*gin.Context)) (*dto.Usage, *types.NewAPIError, *relaycommon.RelayInfo, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set(common.RequestIdKey, "responses-regression-test")
	if writer != nil {
		writer(c)
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "gpt-4o", DisablePing: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gpt-4o"},
	}
	var body strings.Builder
	for _, event := range events {
		body.WriteString("data: " + event + "\n\n")
	}
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body.String())), Header: http.Header{"Content-Type": {"text/event-stream"}}}
	usage, apiErr := OaiResponsesStreamHandler(c, info, resp)
	return usage, apiErr, info, w
}

func TestResponsesDetailedUsageJSONAndStream(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			var usage *dto.Usage
			if stream {
				var apiErr *types.NewAPIError
				usage, apiErr, _, _ = runResponsesRegressionStream(t, []string{`{"type":"response.completed","response":{"status":"completed","usage":` + responsesDetailedUsage + `}}`}, nil)
				require.Nil(t, apiErr)
			} else {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"status":"completed","usage":` + responsesDetailedUsage + `}`)), Header: http.Header{"Content-Type": {"application/json"}}}
				var apiErr *types.NewAPIError
				usage, apiErr = OaiResponsesHandler(c, &relaycommon.RelayInfo{}, resp)
				require.Nil(t, apiErr)
			}
			require.NotNil(t, usage)
			assert.Equal(t, 100, usage.PromptTokens)
			assert.Equal(t, 20, usage.CompletionTokens)
			assert.Equal(t, 120, usage.TotalTokens)
			assert.Equal(t, dto.InputTokenDetails{CachedTokens: 10, CacheWriteTokens: 3, TextTokens: 90, AudioTokens: 2, ImageTokens: 8}, usage.PromptTokensDetails)
			assert.Equal(t, dto.OutputTokenDetails{TextTokens: 11, AudioTokens: 4, ImageTokens: 5, ReasoningTokens: 6}, usage.CompletionTokenDetails)
			require.NotNil(t, usage.BillingUsage)
			require.NotNil(t, usage.BillingUsage.OpenAIUsage)
			assert.Equal(t, usage.PromptTokensDetails, usage.BillingUsage.OpenAIUsage.PromptTokensDetails)
			assert.Equal(t, usage.CompletionTokenDetails, usage.BillingUsage.OpenAIUsage.CompletionTokenDetails)
			assert.Equal(t, dto.BillingUsageSourceOAIResponses, usage.BillingUsage.Source)
		})
	}
}

func TestResponsesImageAccountingOnStreamTermination(t *testing.T) {
	item := `{"type":"image_generation_call","id":"image_1","status":"completed","result":"image-data"}`
	completedImage := `{"type":"response.output_item.done","output_index":0,"item":` + item + `}`
	tests := []struct {
		name      string
		tail      []string
		wantCount int
		wantError bool
	}{
		{"unexpected_eof", nil, 1, true},
		{"completed_duplicate", []string{completedImage, `{"type":"response.completed","response":{"status":"completed","output":[` + item + `]}}`}, 1, false},
		{"failed", []string{`{"type":"response.failed","response":{"status":"failed"}}`}, 0, true},
		{"incomplete", []string{`{"type":"response.incomplete","response":{"status":"incomplete"}}`}, 0, true},
		{"cancelled", []string{`{"type":"response.cancelled","response":{"status":"cancelled"}}`}, 0, true},
		{"canceled", []string{`{"type":"response.canceled","response":{"status":"canceled"}}`}, 0, true},
		{"error", []string{`{"type":"error","message":"upstream failed"}`}, 0, true},
		{"response_error", []string{`{"type":"response.error","message":"upstream failed"}`}, 0, true},
		{"malformed_tail", []string{`{"type":`}, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, apiErr, info, w := runResponsesRegressionStream(t, append([]string{completedImage}, tt.tail...), nil)
			if tt.wantError {
				require.NotNil(t, apiErr)
			} else {
				require.Nil(t, apiErr)
			}
			assert.Contains(t, w.Body.String(), "image-data")
			require.NotNil(t, info.ResponsesUsageInfo)
			assert.Equal(t, tt.wantCount, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
			assert.True(t, info.HasValidOutput())
		})
	}
}

func TestResponsesLargeImageFirstEvent(t *testing.T) {
	image := strings.Repeat("a", helper.MaxStreamPreludeBytes+1024)
	for _, partial := range []bool{true, false} {
		t.Run(map[bool]string{true: "partial", false: "completed"}[partial], func(t *testing.T) {
			event := `{"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"image_1","status":"completed","result":"` + image + `"}}`
			wantCount := 1
			if partial {
				event = `{"type":"response.image_generation_call.partial_image","output_index":0,"partial_image_b64":"` + image + `"}`
				wantCount = 0
			}
			_, apiErr, info, w := runResponsesRegressionStream(t, []string{`{"type":"response.created","response":{"id":"response_1"}}`, event, `{"type":"response.completed","response":{"status":"completed"}}`}, nil)
			require.Nil(t, apiErr)
			assert.Contains(t, w.Body.String(), image)
			assert.Contains(t, w.Body.String(), "response.created")
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, wantCount, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
		})
	}
}

func TestResponsesPartialImageEOFIsNotBillable(t *testing.T) {
	_, apiErr, info, w := runResponsesRegressionStream(t, []string{`{"type":"response.image_generation_call.partial_image","partial_image_b64":"partial-image"}`}, nil)
	require.NotNil(t, apiErr)
	assert.Contains(t, w.Body.String(), "partial-image")
	assert.Zero(t, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}

type cancelAfterImageWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
}

func (w *cancelAfterImageWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	if strings.Contains(string(data), "image-data") {
		w.cancel()
	}
	return n, err
}

func TestResponsesDeliveredImageSurvivesClientCancellation(t *testing.T) {
	_, apiErr, info, w := runResponsesRegressionStream(t, []string{`{"type":"response.output_item.done","output_index":0,"item":{"type":"image_generation_call","id":"image_1","status":"completed","result":"image-data"}}`}, func(c *gin.Context) {
		ctx, cancel := context.WithCancel(c.Request.Context())
		t.Cleanup(cancel)
		c.Request = c.Request.WithContext(ctx)
		c.Writer = &cancelAfterImageWriter{ResponseWriter: c.Writer, cancel: cancel}
	})
	require.NotNil(t, apiErr)
	assert.Contains(t, w.Body.String(), "image-data")
	assert.Equal(t, 1, info.ResponsesUsageInfo.BuiltInTools[dto.BuildInToolImageGeneration].CallCount)
}
