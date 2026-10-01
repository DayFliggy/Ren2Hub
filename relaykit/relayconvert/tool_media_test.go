package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolImageResultPreservesStructureAcrossClaudeResponses(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"fixture","max_tokens":100,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_image","name":"screenshot","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_image","content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"url","url":"https://example.test/screenshot.png"}},{"type":"text","text":"after"}]}]}]}`), &request))
	requirements, err := AnalyzeRequest(&request)
	require.NoError(t, err)
	require.Len(t, requirements.Media, 1)
	assert.True(t, requirements.Media[0].ToolResult)
	plan, err := PlanConversion(requirements, types.RelayFormatOpenAIResponses, types.ConversionLossPolicySafe)
	require.NoError(t, err)
	result, err := ExecuteConversionPlan(context.Background(), nil, plan, &request)
	require.NoError(t, err)
	responseRequest, ok := result.Value.(*dto.OpenAIResponsesRequest)
	require.True(t, ok)
	var items []map[string]any
	require.NoError(t, kitutil.Unmarshal(responseRequest.Input, &items))
	var output []any
	for _, item := range items {
		if item["type"] == "function_call_output" {
			assert.Equal(t, "call_image", item["call_id"])
			output, ok = item["output"].([]any)
			require.True(t, ok, "tool images must remain structured input")
		}
	}
	require.Len(t, output, 3)
	assert.Equal(t, "before", output[0].(map[string]any)["text"])
	assert.Equal(t, "input_image", output[1].(map[string]any)["type"])
	assert.Equal(t, "after", output[2].(map[string]any)["text"])
	back, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, responseRequest)
	require.NoError(t, err)
	encoded, err := kitutil.Marshal(back.Value)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"tool_use_id":"call_image"`)
	assert.Contains(t, string(encoded), `"type":"image"`)
	assert.Contains(t, string(encoded), `"data":"aGVsbG8="`)
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow} {
			_, err := PlanConversion(requirements, target, policy)
			require.NoError(t, err, "%s supports upstream tool-media hoist under %s", target, policy)
		}
	}
}

func TestClaudeToolImageReachesGeminiImagePart(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"fixture","max_tokens":100,"messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_image","name":"screenshot","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_image","content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]}]}]}`), &request))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &request)
	require.NoError(t, err)
	gemini := result.Value.(*dto.GeminiChatRequest)
	var hasFunctionResponse, hasImage bool
	for _, content := range gemini.Contents {
		for _, part := range content.Parts {
			hasFunctionResponse = hasFunctionResponse || part.FunctionResponse != nil
			hasImage = hasImage || part.InlineData != nil && part.InlineData.MimeType == "image/png" && part.InlineData.Data == "aGVsbG8="
		}
	}
	require.True(t, hasFunctionResponse)
	require.True(t, hasImage)
}

func TestToolMediaRejectsUnsupportedSourcesAndRoutes(t *testing.T) {
	for _, test := range []struct {
		name, body string
		request    any
		target     types.RelayFormat
	}{
		{"claude_tool_image_with_unknown_block", `{"model":"fixture","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"YWJj"}},{"type":"unknown","value":"metadata"}]}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"responses_tool_image_with_unknown_block", `{"model":"fixture","input":[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_image","image_url":"data:image/png;base64,YWJj"},{"type":"unknown","value":"metadata"}]}]}`, &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
		{"chat_tool_image_to_responses", `{"model":"fixture","messages":[{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_1","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,YWJj"}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatOpenAIResponses},
		{"gemini_tool_image_to_chat", `{"contents":[{"role":"model","parts":[{"functionCall":{"name":"read","args":{}}}]},{"role":"user","parts":[{"functionResponse":{"name":"read","response":{"result":"image"},"parts":[{"inlineData":{"mimeType":"image/png","data":"YWJj"}}]}}]}]}`, &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"claude_document_to_chat", `{"model":"fixture","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}}]}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"claude_document_to_gemini", `{"model":"fixture","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}}]}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatGemini},
		{"claude_missing_image", `{"model":"fixture","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"image"}]}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatOpenAIResponses},
		{"responses_remote_file_to_chat", `{"model":"fixture","input":[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_file","file_url":"https://example.test/one.pdf"}]}]}`, &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
		{"responses_audio_to_claude", `{"model":"fixture","input":[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}]}]}`, &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			require.Len(t, requirements.Media, 1)
			assert.True(t, requirements.Media[0].ToolResult)
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				plan, err := PlanConversion(requirements, test.target, policy)
				require.Error(t, err, string(policy))
				require.NotEmpty(t, plan.Diagnostics)
				assert.Equal(t, "media_conversion_unavailable", plan.Diagnostics[0].Code)
				_, err = ExecuteConversionPlan(context.Background(), nil, plan, test.request)
				require.Error(t, err, "replaying a rejected plan must not stringify media")
			}
		})
	}
}

func TestClaudeToolDocumentReachesResponsesFilePart(t *testing.T) {
	for _, test := range []struct {
		name, source, field, value string
	}{
		{"inline", `{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}`, "file_data", "data:application/pdf;base64,JVBERg=="},
		{"url", `{"type":"url","url":"https://example.test/one.pdf"}`, "file_url", "https://example.test/one.pdf"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request dto.ClaudeRequest
			body := `{"model":"fixture","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_1","name":"read","input":{}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"document","source":` + test.source + `}]}]}]}`
			require.NoError(t, kitutil.Unmarshal([]byte(body), &request))
			result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, &request)
			require.NoError(t, err)
			responses, ok := result.Value.(*dto.OpenAIResponsesRequest)
			require.True(t, ok)
			var items []map[string]any
			require.NoError(t, kitutil.Unmarshal(responses.Input, &items))
			var output []any
			for _, item := range items {
				if item["type"] == "function_call_output" {
					output, ok = item["output"].([]any)
					require.True(t, ok)
				}
			}
			require.Len(t, output, 1)
			part, ok := output[0].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "input_file", part["type"])
			assert.Equal(t, test.value, part[test.field])
		})
	}
}
