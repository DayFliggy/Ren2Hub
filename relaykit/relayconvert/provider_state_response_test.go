package relayconvert

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const providerStateSecret = "PROVIDER_STATE_SECRET"

// Reasoning signatures (Claude thinking, Gemini thought signatures) cannot be
// replayed through another protocol. A cross-protocol response is still
// delivered: the signature is dropped, never copied, and reported once so
// strict can reject it. Encrypted hosted-tool and citation state keeps its
// native-only rule (response_reaudit_test.go).
func TestCrossProtocolResponseDropsProviderState(t *testing.T) {
	claudeBody := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"thinking","thinking":"plan","signature":"` + providerStateSecret + `"},{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
	geminiBody := `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"checking","thoughtSignature":"` + providerStateSecret + `"},{"functionCall":{"name":"lookup","args":{"q":"x"}},"thoughtSignature":"` + providerStateSecret + `"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`

	cases := []struct {
		from    types.RelayFormat
		body    string
		targets []types.RelayFormat
	}{
		{types.RelayFormatClaude, claudeBody, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini}},
		{types.RelayFormatGemini, geminiBody, []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude}},
	}
	for _, tc := range cases {
		for _, target := range tc.targets {
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow, types.ConversionLossPolicyStrict} {
				t.Run(string(tc.from)+"_to_"+string(target)+"_"+string(policy), func(t *testing.T) {
					info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}, UpstreamModelName: "test-model", ChannelMetaAttached: true}
					result, err := ConvertResponse(context.Background(), info, target, providerStateResponse(t, tc.from, tc.body))
					if policy == types.ConversionLossPolicyStrict {
						require.ErrorContains(t, err, "reasoning signatures cannot cross protocols")
						return
					}
					require.NoError(t, err)
					encoded, err := kitutil.Marshal(result.Value)
					require.NoError(t, err)
					assert.NotContains(t, string(encoded), providerStateSecret)
					assert.Equal(t, 1, countDiagnostics(result.Diagnostics, "provider_state_dropped"))
				})
			}
		}
	}
}

func TestCrossProtocolStreamDropsProviderStateOnce(t *testing.T) {
	streams := []struct {
		from    types.RelayFormat
		events  []string
		targets []types.RelayFormat
	}{
		{
			from: types.RelayFormatClaude,
			events: []string{
				`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","usage":{"input_tokens":3,"output_tokens":0}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"plan"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"` + providerStateSecret + `"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`,
			},
			targets: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini},
		},
		{
			from: types.RelayFormatGemini,
			events: []string{
				`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"checking","thoughtSignature":"` + providerStateSecret + `"}]}}]}`,
				`{"candidates":[{"index":0,"content":{"role":"model","parts":[{"functionCall":{"name":"lookup","args":{"q":"x"}},"thoughtSignature":"` + providerStateSecret + `"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`,
			},
			targets: []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude},
		},
	}
	for _, stream := range streams {
		for _, target := range stream.targets {
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow, types.ConversionLossPolicyStrict} {
				t.Run(string(stream.from)+"_to_"+string(target)+"_"+string(policy), func(t *testing.T) {
					info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}, UpstreamModelName: "test-model", ChannelMetaAttached: true}
					state, err := NewResponseStreamState(stream.from, target, ResponseStreamOptions{ID: "stream_1", Model: "test-model"})
					require.NoError(t, err)
					var results []ResponseResult
					var streamErr error
					for _, event := range stream.events {
						chunk, err := ConvertStreamResponseChunk(context.Background(), info, state, providerStateResponse(t, stream.from, event))
						if err != nil {
							streamErr = err
							break
						}
						results = append(results, chunk...)
					}
					if policy == types.ConversionLossPolicyStrict {
						require.ErrorContains(t, streamErr, "reasoning signatures cannot cross protocols")
						return
					}
					require.NoError(t, streamErr)
					final, err := FinalizeStreamResponse(context.Background(), info, state)
					require.NoError(t, err)
					results = append(results, final...)
					encoded, err := kitutil.Marshal(streamValues(results))
					require.NoError(t, err)
					assert.NotContains(t, string(encoded), providerStateSecret)
					assert.Contains(t, string(encoded), firstText(stream.from))
					var diagnostics []types.ConversionDiagnostic
					for _, result := range results {
						diagnostics = append(diagnostics, result.Diagnostics...)
					}
					assert.Equal(t, 1, countDiagnostics(diagnostics, "provider_state_dropped"))
					assert.Equal(t, 1, countDiagnostics(state.Diagnostics(), "provider_state_dropped"))
				})
			}
		}
	}
}

func providerStateResponse(t *testing.T, from types.RelayFormat, body string) any {
	t.Helper()
	var response any
	switch from {
	case types.RelayFormatClaude:
		response = &dto.ClaudeResponse{}
	case types.RelayFormatGemini:
		response = &dto.GeminiChatResponse{}
	default:
		t.Fatalf("unsupported source %s", from)
	}
	require.NoError(t, kitutil.Unmarshal([]byte(body), response))
	return response
}

func firstText(from types.RelayFormat) string {
	if from == types.RelayFormatGemini {
		return "checking"
	}
	return "hello"
}

func countDiagnostics(diagnostics []types.ConversionDiagnostic, code string) int {
	count := 0
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			count++
		}
	}
	return count
}

// Claude's redacted_thinking block is encrypted reasoning with no visible
// text, so it follows the signature rule: the cross-protocol response is
// delivered without its data and reported once as provider_state_dropped,
// which safe and allow pass and strict rejects, in JSON and in streams.
func TestCrossProtocolResponseDropsRedactedThinking(t *testing.T) {
	const body = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[{"type":"redacted_thinking","data":"` + providerStateSecret + `"},{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`
	events := []string{
		`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","usage":{"input_tokens":3,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"redacted_thinking","data":"` + providerStateSecret + `"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"hello"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
		`{"type":"message_stop"}`,
	}
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow, types.ConversionLossPolicyStrict} {
			t.Run(string(target)+"_"+string(policy), func(t *testing.T) {
				info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}, UpstreamModelName: "test-model", ChannelMetaAttached: true}
				result, err := ConvertResponse(context.Background(), info, target, providerStateResponse(t, types.RelayFormatClaude, body))
				if policy == types.ConversionLossPolicyStrict {
					require.ErrorContains(t, err, "reasoning signatures cannot cross protocols")
				} else {
					require.NoError(t, err)
					encoded, err := kitutil.Marshal(result.Value)
					require.NoError(t, err)
					assert.NotContains(t, string(encoded), providerStateSecret)
					assert.Contains(t, string(encoded), "hello")
					assert.Equal(t, 1, countDiagnostics(result.Diagnostics, "provider_state_dropped"))
					assert.Zero(t, countDiagnostics(result.Diagnostics, "continuation_state_lost"))
				}

				state, err := NewResponseStreamState(types.RelayFormatClaude, target, ResponseStreamOptions{ID: "stream_1", Model: "test-model"})
				require.NoError(t, err)
				var results []ResponseResult
				var streamErr error
				for _, event := range events {
					chunk, err := ConvertStreamResponseChunk(context.Background(), info, state, providerStateResponse(t, types.RelayFormatClaude, event))
					if err != nil {
						streamErr = err
						break
					}
					results = append(results, chunk...)
				}
				if policy == types.ConversionLossPolicyStrict {
					require.ErrorContains(t, streamErr, "reasoning signatures cannot cross protocols")
					return
				}
				require.NoError(t, streamErr)
				final, err := FinalizeStreamResponse(context.Background(), info, state)
				require.NoError(t, err)
				results = append(results, final...)
				encoded, err := kitutil.Marshal(streamValues(results))
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), providerStateSecret)
				assert.Contains(t, string(encoded), "hello")
				assert.Equal(t, 1, countDiagnostics(state.Diagnostics(), "provider_state_dropped"))
			})
		}
	}
}

// Manual extended thinking always returns signed thinking blocks, which a
// cross-protocol response must drop. Strict rejects that request before send
// rather than after the upstream has billed the response. Adaptive thinking,
// native Claude requests and the safe and allow policies still send it.
func TestStrictRejectsCrossProtocolManualThinkingBeforeSend(t *testing.T) {
	const (
		chatBody      = `{"model":"%s","max_tokens":4096,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`
		responsesBody = `{"model":"%s","max_output_tokens":4096,"input":"hello","reasoning":{"effort":"high"}}`
		claudeBody    = `{"model":"%s","max_tokens":4096,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":2048}}`
		manualModel   = "claude-3-7-sonnet-20250219"
	)
	cases := []struct {
		name     string
		request  any
		body     string
		model    string
		policy   types.ConversionLossPolicy
		thinking string
		rejected bool
	}{
		{"chat manual strict", &dto.GeneralOpenAIRequest{}, chatBody, manualModel, types.ConversionLossPolicyStrict, "enabled", true},
		{"responses manual strict", &dto.OpenAIResponsesRequest{}, responsesBody, manualModel, types.ConversionLossPolicyStrict, "enabled", true},
		{"chat adaptive strict", &dto.GeneralOpenAIRequest{}, chatBody, "claude-opus-5-5", types.ConversionLossPolicyStrict, "adaptive", false},
		{"chat manual safe", &dto.GeneralOpenAIRequest{}, chatBody, manualModel, types.ConversionLossPolicySafe, "enabled", false},
		{"chat manual allow", &dto.GeneralOpenAIRequest{}, chatBody, manualModel, types.ConversionLossPolicyAllow, "enabled", false},
		{"claude manual strict", &dto.ClaudeRequest{}, claudeBody, manualModel, types.ConversionLossPolicyStrict, "enabled", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(fmt.Sprintf(tc.body, tc.model)), tc.request))
			info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: tc.policy}, UpstreamModelName: tc.model, ChannelMetaAttached: true}
			result, err := ConvertRequest(context.Background(), info, types.RelayFormatClaude, tc.request)
			if tc.rejected {
				var loss *types.ConversionLossError
				require.ErrorAs(t, err, &loss)
				require.Len(t, loss.Diagnostics, 1)
				assert.Equal(t, "provider_state_dropped", loss.Diagnostics[0].Code)
				assert.Equal(t, "thinking", loss.Diagnostics[0].Path)
				return
			}
			require.NoError(t, err)
			claudeRequest, ok := result.Value.(*dto.ClaudeRequest)
			require.True(t, ok)
			require.NotNil(t, claudeRequest.Thinking)
			assert.Equal(t, tc.thinking, claudeRequest.Thinking.Type)
			assert.Zero(t, countDiagnostics(result.Diagnostics, "provider_state_dropped"))
		})
	}
}
