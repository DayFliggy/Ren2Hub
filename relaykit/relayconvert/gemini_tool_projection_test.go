package relayconvert

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func geminiIdentityHistory(names ...string) *dto.GeminiChatRequest {
	calls := dto.GeminiChatContent{Role: "model"}
	results := dto.GeminiChatContent{Role: "user"}
	for i, name := range names {
		id := "call_" + string(rune('a'+i))
		rawID, _ := json.Marshal(id)
		calls.Parts = append(calls.Parts, dto.GeminiPart{FunctionCall: &dto.FunctionCall{ID: id, FunctionName: name, Arguments: map[string]any{}}})
		results.Parts = append(results.Parts, dto.GeminiPart{FunctionResponse: &dto.GeminiFunctionResponse{ID: rawID, Name: name, Response: map[string]any{"result": "done"}}})
	}
	return &dto.GeminiChatRequest{Contents: []dto.GeminiChatContent{calls, results}}
}

func TestGeminiNameOnlyProjectionProtectsToolIdentity(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		t.Run(string(policy), func(t *testing.T) {
			request := geminiIdentityHistory("echo", "other")
			result, err := ProjectGeminiToolHistoryWithoutIDs(request, policy)
			require.Len(t, result.Diagnostics, 1)
			assert.Equal(t, "tool_ids_projected_to_names", result.Diagnostics[0].Code)
			if policy == types.ConversionLossPolicyStrict {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				projected := result.Value.(*dto.GeminiChatRequest)
				assert.Empty(t, projected.Contents[0].Parts[0].FunctionCall.ID)
				assert.Empty(t, projected.Contents[1].Parts[0].FunctionResponse.ID)
			}
			assert.Equal(t, "call_a", request.Contents[0].Parts[0].FunctionCall.ID, "projection must not modify the client history")
			assert.JSONEq(t, `"call_a"`, string(request.Contents[1].Parts[0].FunctionResponse.ID))

			result, err = ProjectGeminiToolHistoryWithoutIDs(geminiIdentityHistory("echo", "echo"), policy)
			require.Error(t, err, "allow must not bypass ambiguous name correlation")
			assert.Equal(t, "tool_identity_required", result.Diagnostics[0].Code)
		})
	}
}

func TestNameOnlyRequirementsUseSameHistoryAcrossProtocols(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
		for _, names := range [][]string{{"echo", "other"}, {"echo", "echo"}} {
			t.Run(string(format)+"/"+names[1], func(t *testing.T) {
				native := geminiIdentityHistory(names...)
				maxTokens := uint(32)
				native.GenerationConfig.MaxOutputTokens = &maxTokens
				meta := &convmeta.Values{UpstreamModelName: "test", ChannelMetaAttached: true}
				result, err := ConvertRequest(context.Background(), meta, format, native)
				require.NoError(t, err)
				requirements, err := AnalyzeRequest(result.Value)
				require.NoError(t, err)
				require.NotNil(t, requirements.ToolIdentity)
				assert.True(t, requirements.ToolIdentity.ExplicitIDs)
				assert.False(t, requirements.ToolIdentity.InvalidHistory)
				assert.Equal(t, names[0] == names[1], requirements.ToolIdentity.RequiresIDs)
				data, err := json.Marshal(requirements.ToolIdentity)
				require.NoError(t, err)
				assert.NotContains(t, string(data), "call_a")
				assert.NotContains(t, string(data), "done")
			})
		}
	}
}

func TestGeminiNameOnlyProjectionValidatesOriginalHistory(t *testing.T) {
	request := geminiIdentityHistory("echo")
	request.Contents[1].Parts[0].FunctionResponse.ID = []byte(`"unknown"`)
	result, err := ProjectGeminiToolHistoryWithoutIDs(request, types.ConversionLossPolicyAllow)
	require.Error(t, err, "removing an unknown explicit ID must not repair an invalid result")
	assert.Equal(t, "invalid_tool_history", result.Diagnostics[0].Code)

	request = geminiIdentityHistory("echo")
	request.Contents = append(request.Contents, geminiIdentityHistory("echo").Contents...)
	request.Contents[2].Parts[0].FunctionCall.ID = "call_b"
	request.Contents[3].Parts[0].FunctionResponse.ID = []byte(`"call_b"`)
	_, err = ProjectGeminiToolHistoryWithoutIDs(request, types.ConversionLossPolicySafe)
	require.NoError(t, err, "sequential calls with the same name remain unambiguous")

	removeGeminiToolIDs(request)
	result, err = ProjectGeminiToolHistoryWithoutIDs(request, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
	assert.Empty(t, result.Diagnostics)
}

func TestToolIdentityAnalysisIgnoresTextAndMedia(t *testing.T) {
	request := &dto.ClaudeRequest{Messages: []dto.ClaudeMessage{
		{Role: "user", Content: "hello"},
		{Role: "user", Content: []dto.ClaudeMediaMessage{{Type: "image", Source: &dto.ClaudeMessageSource{Type: "url", Url: "https://must-not-fetch.invalid/image.png"}}}},
	}}
	requirements, err := AnalyzeRequest(request)
	require.NoError(t, err)
	assert.Nil(t, requirements.ToolIdentity)
	_, err = ProjectGeminiToolHistoryWithoutIDs(&dto.GeminiChatRequest{}, types.ConversionLossPolicy("invalid"))
	require.Error(t, err, "empty histories must still validate the public projection policy")
}
