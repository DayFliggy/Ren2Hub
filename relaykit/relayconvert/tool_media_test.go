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
