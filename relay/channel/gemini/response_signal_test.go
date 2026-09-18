package gemini

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiChatStreamHandlerSeparatesThoughtAndAnswerAcrossFrameBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frames []string
	}{
		{name: "mixed frame", frames: []string{`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"thought":true,"text":"Brief explanation."},{"text":"Final answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`}},
		{name: "separate frames", frames: []string{
			`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"thought":true,"text":"Brief explanation."}]}}]}`,
			`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"Final answer"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "gemini-test"}}
			body := "data: " + strings.Join(tc.frames, "\n\ndata: ") + "\n\n"
			usage, apiErr := GeminiChatStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 12, usage.TotalTokens)
			var answer, reasoning strings.Builder
			for line := range strings.SplitSeq(recorder.Body.String(), "\n") {
				data, ok := strings.CutPrefix(line, "data: ")
				if !ok || data == "[DONE]" {
					continue
				}
				var chunk dto.ChatCompletionsStreamResponse
				require.NoError(t, common.UnmarshalJsonStr(data, &chunk))
				for _, choice := range chunk.Choices {
					answer.WriteString(choice.Delta.GetContentString())
					reasoning.WriteString(choice.Delta.GetReasoningContent())
				}
			}
			assert.Equal(t, "Final answer", answer.String())
			assert.Equal(t, "Brief explanation.", reasoning.String())
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
			assert.Contains(t, recorder.Body.String(), `"role":"assistant"`)
		})
	}
}

func TestGeminiGenerationSignalsBeforeClientCommit(t *testing.T) {
	for _, handler := range []struct {
		name string
		run  func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"native", GeminiTextGenerationHandler},
		{"chat", GeminiChatHandler},
		{"responses", GeminiResponsesHandler},
	} {
		for _, tc := range []struct {
			name, body string
			status     int
			penalize   bool
		}{
			{"quota", `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, 429, true},
			{"status_only", `{"error":{"message":"quota","status":"RESOURCE_EXHAUSTED"}}`, 429, true},
			{"bad_request", `{"error":{"code":400,"message":"invalid input","status":"INVALID_ARGUMENT"}}`, 400, false},
			{"empty_object", `{}`, 502, true},
			{"null", `null`, 502, true},
			{"array", `[]`, 502, true},
			{"empty_candidates", `{"candidates":[]}`, 502, true},
			{"empty_candidate", `{"candidates":[{}]}`, 502, true},
			{"empty_function_call", `{"candidates":[{"content":{"parts":[{"functionCall":{}}]}}]}`, 502, true},
			{"unknown_finish", `{"candidates":[{"finishReason":"NOT_A_REAL_REASON"}]}`, 502, true},
			{"unknown_finish_with_content", `{"candidates":[{"finishReason":"NOT_A_REAL_REASON","content":{"parts":[{"text":"partial"}]}}]}`, 502, true},
			{"empty_body", ``, 502, true},
			{"invalid_json", `{`, 502, true},
			{"prompt_policy", `{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{"promptTokenCount":2,"totalTokenCount":2}}`, 400, false},
			{"candidate_policy", `{"candidates":[{"finishReason":"SAFETY"}]}`, 400, false},
		} {
			t.Run(handler.name+"/"+tc.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				_, apiErr := handler.run(c, newGeminiResponsesRelayInfo(false), &http.Response{StatusCode: 200,
					Header: http.Header{"Retry-After": {"7"}}, Body: io.NopCloser(strings.NewReader(tc.body))})
				require.NotNil(t, apiErr)
				require.Equal(t, tc.status, apiErr.StatusCode)
				require.Equal(t, 7*time.Second, apiErr.RetryAfter)
				require.False(t, c.Writer.Written())
				require.Empty(t, recorder.Body.String())
				classification := service.ClassifyAttemptOutcome(service.AttemptOutcome{Origin: service.AttemptOriginProvider, StatusCode: apiErr.StatusCode, ProviderCode: string(apiErr.GetErrorCode()), Message: apiErr.Error()})
				require.Equal(t, tc.penalize, classification.MarkChannelModel)
			})
		}
	}
}

func TestGeminiStreamSignalsPreserveUsageAndFailure(t *testing.T) {
	for _, handler := range []struct {
		name string
		run  func(*gin.Context, *relaycommon.RelayInfo, *http.Response) (*dto.Usage, *types.NewAPIError)
	}{
		{"native", GeminiTextGenerationStreamHandler},
		{"chat", GeminiChatStreamHandler},
		{"responses", GeminiResponsesStreamHandler},
	} {
		for _, partial := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/partial=%t", handler.name, partial), func(t *testing.T) {
				body := ""
				if partial {
					body = "data: " + `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":3,"totalTokenCount":5}}` + "\n\n"
				}
				body += "data: " + `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}` + "\n\n"
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				info := newGeminiResponsesRelayInfo(true)
				if handler.name != "responses" {
					info.RelayFormat = types.RelayFormatOpenAI
				}
				usage, apiErr := handler.run(c, info, &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))})
				require.NotNil(t, apiErr)
				require.Equal(t, 429, apiErr.StatusCode)
				require.Equal(t, types.ErrorCode("RESOURCE_EXHAUSTED"), apiErr.GetErrorCode())
				require.False(t, info.StreamStatus.IsNormalEnd())
				require.Equal(t, partial, info.HasValidOutput())
				require.NotContains(t, recorder.Body.String(), "response.completed")
				require.NotContains(t, recorder.Body.String(), "[DONE]")
				if partial {
					require.Equal(t, 5, usage.TotalTokens)
					require.Contains(t, recorder.Body.String(), "partial")
				} else {
					require.False(t, c.Writer.Written())
					require.Empty(t, recorder.Body.String())
				}
			})
		}
	}
}

func TestGeminiGenerationFinishAndMetadataBoundaries(t *testing.T) {
	for _, reason := range []string{"STOP", "MAX_TOKENS", "SAFETY", "RECITATION", "OTHER", "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS"} {
		t.Run(reason, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			body := fmt.Sprintf(`{"candidates":[{"content":{"role":"model","parts":[{"text":"partial"}]},"finishReason":%q}]}`, reason)
			_, apiErr := parseGeminiGenerationResponse(c, []byte(body), false, false)
			require.Nil(t, apiErr)
			_, apiErr = parseGeminiGenerationResponse(c, []byte(fmt.Sprintf(`{"candidates":[{"finishReason":%q}]}`, reason)), true, true)
			require.Nil(t, apiErr)
		})
	}
	for _, body := range []string{`{"candidates":[{"finishReason":"MAX_TOKENS"}]}`, `{"candidates":[{"finishReason":"STOP"}],"promptFeedback":{"blockReason":"BLOCK_REASON_UNSPECIFIED"}}`} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		_, apiErr := parseGeminiGenerationResponse(c, []byte(body), false, false)
		require.Nil(t, apiErr)
	}
	for _, body := range []string{"data: [DONE]\n\n", "data: {}\n\n", "data: " + `{"candidates":[]}` + "\n\n", "data: " + `{"candidates":[{}]}` + "\n\ndata: [DONE]\n\n", "data: " + `{"candidates":[{"finishReason":"NOT_A_REAL_REASON"}]}` + "\n\n", "data: " + `{"usageMetadata":{"promptTokenCount":2}}` + "\n\ndata: [DONE]\n\n"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		info := newGeminiResponsesRelayInfo(true)
		_, apiErr := GeminiTextGenerationStreamHandler(c, info, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))})
		require.NotNil(t, apiErr)
		require.Equal(t, 502, apiErr.StatusCode)
		require.False(t, c.Writer.Written())
	}
}
