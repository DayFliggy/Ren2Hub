package openai

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const chatDeliveryHello = `{"id":"chat_delivery","model":"test-model","created":123,"choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":null}]}`
const chatDeliveryTail = `{"choices":[{"index":0,"delta":{"content":" tail"},"finish_reason":null}]}`
const chatDeliveryStop = `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`
const chatDeliveryUsage = `{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`

func chatDeliverySSE(frames ...string) string {
	return "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
}

func runChatDelivery(t *testing.T, format types.RelayFormat, writer http.ResponseWriter, body io.ReadCloser, configure func(*relaycommon.RelayInfo)) (*gin.Context, *relaycommon.RelayInfo, *dto.Usage, *types.NewAPIError) {
	t.Helper()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{
		RelayFormat: format, RelayMode: relayconstant.RelayModeChatCompletions,
		DisablePing: true, IsStream: true,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "test-model"},
	}
	info.SetEstimatePromptTokens(10)
	if configure != nil {
		configure(info)
	}
	usage, apiErr := OaiStreamHandler(c, info, &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body})
	return c, info, usage, apiErr
}

func chatDeliveryFrames(body string) []gjson.Result {
	var frames []gjson.Result
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data: {") {
			frames = append(frames, gjson.Parse(strings.TrimPrefix(line, "data: ")))
		}
	}
	return frames
}

func chatDeliveryText(body string, format types.RelayFormat) string {
	var text strings.Builder
	for _, frame := range chatDeliveryFrames(body) {
		switch format {
		case types.RelayFormatOpenAI:
			for _, choice := range frame.Get("choices").Array() {
				text.WriteString(choice.Get("delta.content").String())
			}
		case types.RelayFormatClaude:
			if frame.Get("type").String() == "content_block_delta" {
				text.WriteString(frame.Get("delta.text").String())
			}
		case types.RelayFormatGemini:
			for _, candidate := range frame.Get("candidates").Array() {
				for _, part := range candidate.Get("content.parts").Array() {
					text.WriteString(part.Get("text").String())
				}
			}
		}
	}
	return text.String()
}

func chatDeliveryTerminals(body string, format types.RelayFormat) int {
	if format == types.RelayFormatOpenAI {
		return strings.Count(body, "data: [DONE]")
	}
	count := 0
	for _, frame := range chatDeliveryFrames(body) {
		if format == types.RelayFormatClaude && frame.Get("type").String() == "message_stop" {
			count++
		}
		if format == types.RelayFormatGemini {
			for _, candidate := range frame.Get("candidates").Array() {
				if candidate.Get("finishReason").String() != "" {
					count++
				}
			}
		}
	}
	return count
}

func TestChatStreamDeliversCurrentFrameWithoutDuplicateTail(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, tc := range []struct {
			name     string
			frames   []string
			want     string
			complete bool
		}{
			{"single_delta_eof", []string{chatDeliveryHello}, "hello", false},
			{"consecutive_delta_eof", []string{chatDeliveryHello, chatDeliveryTail}, "hello tail", false},
			{"done_without_finish_frame", []string{chatDeliveryHello, "[DONE]"}, "hello", false},
			{"normal_finish", []string{chatDeliveryHello, chatDeliveryTail, chatDeliveryStop, "[DONE]"}, "hello tail", true},
			{"usage_only_tail", []string{chatDeliveryHello, chatDeliveryStop, chatDeliveryUsage, "[DONE]"}, "hello", true},
			{"usage_without_terminal", []string{chatDeliveryHello, chatDeliveryUsage}, "hello", false},
			{"content_and_finish_share_frame", []string{`{"id":"chat_delivery","choices":[{"index":0,"delta":{"content":"hello"},"finish_reason":"length"}]}`, chatDeliveryUsage, "[DONE]"}, "hello", true},
		} {
			t.Run(string(format)+"/"+tc.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				_, info, usage, apiErr := runChatDelivery(t, format, recorder, io.NopCloser(strings.NewReader(chatDeliverySSE(tc.frames...))), nil)
				assert.Equal(t, tc.want, chatDeliveryText(recorder.Body.String(), format))
				assert.True(t, info.HasValidOutput())
				require.NotNil(t, usage)
				if tc.complete {
					require.Nil(t, apiErr)
					assert.Equal(t, 1, chatDeliveryTerminals(recorder.Body.String(), format))
				} else {
					require.NotNil(t, apiErr)
					assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
					assert.Equal(t, 0, chatDeliveryTerminals(recorder.Body.String(), format))
				}
				if strings.Contains(tc.name, "usage") {
					assert.Equal(t, 13, usage.TotalTokens)
				}
			})
		}
	}
}

func TestChatStreamRolePreludeRemainsUncommittedOnEOF(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, info, _, apiErr := runChatDelivery(t, format, recorder, io.NopCloser(strings.NewReader(chatDeliverySSE(`{"id":"chat_role","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`))), nil)
			require.NotNil(t, apiErr)
			assert.False(t, c.Writer.Written())
			assert.False(t, info.HasValidOutput())
			assert.Empty(t, recorder.Body.String())
		})
	}
}

type chatDeliveryFailureBody struct {
	io.Reader
	err error
}

func (body chatDeliveryFailureBody) Read(p []byte) (int, error) {
	n, err := body.Reader.Read(p)
	if err == io.EOF {
		return n, body.err
	}
	return n, err
}

func (chatDeliveryFailureBody) Close() error { return nil }

func TestChatStreamReadFailureDoesNotFinalizeConvertedResponse(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := chatDeliveryFailureBody{Reader: strings.NewReader(chatDeliverySSE(chatDeliveryHello, chatDeliveryStop, chatDeliveryUsage)), err: io.ErrUnexpectedEOF}
			_, info, usage, apiErr := runChatDelivery(t, format, recorder, body, nil)
			require.NotNil(t, apiErr)
			assert.ErrorIs(t, info.StreamStatus.Err(), io.ErrUnexpectedEOF)
			assert.Equal(t, "hello", chatDeliveryText(recorder.Body.String(), format))
			assert.Equal(t, 0, chatDeliveryTerminals(recorder.Body.String(), format))
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, 13, usage.TotalTokens)
		})
	}
}

type chatDeliveryFailureWriter struct {
	*httptest.ResponseRecorder
	failAt int
	writes int
}

func (writer *chatDeliveryFailureWriter) Write(p []byte) (int, error) {
	writer.writes++
	if writer.writes >= writer.failAt {
		return 0, io.ErrClosedPipe
	}
	return writer.ResponseRecorder.Write(p)
}

func (writer *chatDeliveryFailureWriter) WriteString(value string) (int, error) {
	return writer.Write([]byte(value))
}

func TestChatStreamDownstreamWriteFailureStopsAndPropagates(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, failAt := range []int{1, 2} {
			t.Run(string(format)+string(rune('0'+failAt)), func(t *testing.T) {
				writer := &chatDeliveryFailureWriter{ResponseRecorder: httptest.NewRecorder(), failAt: failAt}
				c, info, _, apiErr := runChatDelivery(t, format, writer, io.NopCloser(strings.NewReader(chatDeliverySSE(chatDeliveryHello, chatDeliveryTail, chatDeliveryStop, "[DONE]"))), nil)
				require.NotNil(t, apiErr)
				assert.True(t, info.StreamStatus.EndReason == relaycommon.StreamEndReasonClientGone || errors.Is(helper.StreamOutputError(c), helper.ErrStreamClientWrite))
				assert.Equal(t, failAt, writer.writes)
				assert.Equal(t, 0, chatDeliveryTerminals(writer.Body.String(), format))
				assert.Equal(t, failAt > 1, info.HasValidOutput())
			})
		}
	}
}

func TestChatStreamAudioUsageSurvivesLaterEmptyFrame(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := chatDeliverySSE(chatDeliveryHello, chatDeliveryUsage, chatDeliveryStop, "[DONE]")
	_, _, usage, apiErr := runChatDelivery(t, types.RelayFormatOpenAI, recorder, io.NopCloser(strings.NewReader(body)), func(info *relaycommon.RelayInfo) {
		info.UpstreamModelName = "gpt-4o-audio-preview"
		info.ShouldIncludeUsage = true
	})
	require.Nil(t, apiErr)
	assert.Equal(t, 13, usage.TotalTokens)
	assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"total_tokens":13`))
	assert.Equal(t, "hello", chatDeliveryText(recorder.Body.String(), types.RelayFormatOpenAI))
}

func TestChatStreamRejectsInvalidUsageWithoutCompleting(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, tc := range []struct {
			name  string
			usage string
		}{
			{"negative", `{"prompt_tokens":10,"completion_tokens":-1,"total_tokens":9}`},
			{"inconsistent", `{"prompt_tokens":10,"completion_tokens":3,"total_tokens":1}`},
		} {
			t.Run(string(format)+"/"+tc.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				body := chatDeliverySSE(chatDeliveryHello, `{"choices":[],"usage":`+tc.usage+`}`, "[DONE]")
				_, info, usage, apiErr := runChatDelivery(t, format, recorder, io.NopCloser(strings.NewReader(body)), func(info *relaycommon.RelayInfo) { info.ShouldIncludeUsage = true })
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				assert.Equal(t, "hello", chatDeliveryText(recorder.Body.String(), format))
				assert.Equal(t, 0, chatDeliveryTerminals(recorder.Body.String(), format))
				assert.True(t, info.HasValidOutput())
				require.NotNil(t, usage)
				assert.Equal(t, "estimated", usage.UsageSource)
				assert.NotContains(t, recorder.Body.String(), tc.usage)
			})
		}
	}
}

func TestChatStreamThinkingToContentPreservesLastDelta(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := chatDeliverySSE(`{"id":"chat_think","choices":[{"index":0,"delta":{"reasoning_content":"reason"}}]}`, chatDeliveryHello, chatDeliveryTail)
	_, _, _, apiErr := runChatDelivery(t, types.RelayFormatOpenAI, recorder, io.NopCloser(strings.NewReader(body)), func(info *relaycommon.RelayInfo) {
		info.ChannelSetting.ThinkingToContent = true
		info.ThinkingContentInfo.IsFirstThinkingContent = true
	})
	require.NotNil(t, apiErr)
	assert.Equal(t, "<think>\nreason\n</think>\nhello tail", chatDeliveryText(recorder.Body.String(), types.RelayFormatOpenAI))
	assert.NotContains(t, recorder.Body.String(), "reasoning_content")
	assert.NotContains(t, recorder.Body.String(), "[DONE]")
}

func TestChatStreamPreservesChoicesAndParallelTools(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini} {
		t.Run(string(format)+"/multiple_choices", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := chatDeliverySSE(`{"id":"chat_multi","choices":[{"index":0,"delta":{"content":"first"}},{"index":1,"delta":{"content":"second"}}]}`,
				`{"choices":[{"index":0,"delta":{},"finish_reason":"stop"},{"index":1,"delta":{},"finish_reason":"length"}]}`, chatDeliveryUsage, "[DONE]")
			_, info, _, apiErr := runChatDelivery(t, format, recorder, io.NopCloser(strings.NewReader(body)), nil)
			require.Nil(t, apiErr)
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, "firstsecond", chatDeliveryText(recorder.Body.String(), format))
			finish := map[int]string{}
			for _, frame := range chatDeliveryFrames(recorder.Body.String()) {
				path, reasonPath := "choices", "finish_reason"
				if format == types.RelayFormatGemini {
					path, reasonPath = "candidates", "finishReason"
				}
				for _, choice := range frame.Get(path).Array() {
					if reason := choice.Get(reasonPath).String(); reason != "" {
						finish[int(choice.Get("index").Int())] = reason
					}
				}
			}
			if format == types.RelayFormatGemini {
				assert.Equal(t, map[int]string{0: "STOP", 1: "MAX_TOKENS"}, finish)
			} else {
				assert.Equal(t, map[int]string{0: "stop", 1: "length"}, finish)
			}
		})
	}
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format)+"/parallel_tools", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			body := chatDeliverySSE(
				`{"id":"chat_tools","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{\"value\":"}},{"index":1,"id":"call_b","type":"function","function":{"name":"echo","arguments":"{\"value\":"}}]}}]}`,
				`{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"0}"}},{"index":1,"function":{"arguments":"false}"}}]},"finish_reason":"tool_calls"}]}`,
				chatDeliveryUsage, "[DONE]")
			_, info, _, apiErr := runChatDelivery(t, format, recorder, io.NopCloser(strings.NewReader(body)), nil)
			require.Nil(t, apiErr)
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, 1, chatDeliveryTerminals(recorder.Body.String(), format))
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"call_a"`))
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"call_b"`))
			if format == types.RelayFormatGemini {
				frames := chatDeliveryFrames(recorder.Body.String())
				require.Len(t, frames, 1, "partial tool JSON must not commit empty candidate events")
				assert.Equal(t, "0", frames[0].Get("candidates.0.content.parts.0.functionCall.args.value").String())
				assert.Equal(t, "false", frames[0].Get("candidates.0.content.parts.1.functionCall.args.value").String())
			}
		})
	}
}

func TestChatStreamIncompleteGeminiToolDoesNotCommitEmptyCandidate(t *testing.T) {
	recorder := httptest.NewRecorder()
	body := chatDeliverySSE(`{"id":"chat_tools","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_a","type":"function","function":{"name":"echo","arguments":"{"}}]}}]}`)
	c, info, _, apiErr := runChatDelivery(t, types.RelayFormatGemini, recorder, io.NopCloser(strings.NewReader(body)), nil)
	require.NotNil(t, apiErr)
	assert.False(t, c.Writer.Written())
	assert.False(t, info.HasValidOutput())
	assert.Empty(t, recorder.Body.String())
}

func TestChatStreamDownstreamTerminalWriteFailure(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			writer := &chatDeliveryFailureWriter{ResponseRecorder: httptest.NewRecorder(), failAt: 2}
			c, info, _, apiErr := runChatDelivery(t, format, writer, io.NopCloser(strings.NewReader(chatDeliverySSE(chatDeliveryHello, chatDeliveryStop, "[DONE]"))), nil)
			require.NotNil(t, apiErr)
			assert.ErrorIs(t, helper.StreamOutputError(c), helper.ErrStreamClientWrite)
			assert.Equal(t, "hello", chatDeliveryText(writer.Body.String(), format))
			assert.Equal(t, 0, chatDeliveryTerminals(writer.Body.String(), format))
			assert.Equal(t, 2, writer.writes)
			assert.True(t, info.HasValidOutput())
		})
	}
}

func TestChatStreamMalformedFrameDoesNotDiscardDeliveredContent(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		t.Run(string(format), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			_, info, _, apiErr := runChatDelivery(t, format, recorder, io.NopCloser(strings.NewReader(chatDeliverySSE(chatDeliveryHello, "{invalid", "[DONE]"))), nil)
			require.NotNil(t, apiErr)
			assert.Equal(t, "hello", chatDeliveryText(recorder.Body.String(), format))
			assert.True(t, info.HasValidOutput())
			assert.Equal(t, 0, chatDeliveryTerminals(recorder.Body.String(), format))
		})
	}
}
