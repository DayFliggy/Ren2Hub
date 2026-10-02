package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCustomToolsPreserveDefinitionsChoiceAndHistoryAcrossTargets(t *testing.T) {
	for _, target := range []struct {
		format                                      types.RelayFormat
		namePath, inputPath, choicePath, resultPath string
	}{
		{types.RelayFormatOpenAI, "tools.0.function.name", "messages.0.tool_calls.0.function.arguments", "tool_choice.function.name", "messages.1.tool_call_id"},
		{types.RelayFormatClaude, "tools.0.name", "messages.1.content.0.input", "tool_choice.name", "messages.2.content.0.tool_use_id"},
		{types.RelayFormatGemini, "tools.0.functionDeclarations.0.name", "contents.0.parts.0.functionCall.args", "toolConfig.functionCallingConfig.allowedFunctionNames.0", "contents.1.parts.0.functionResponse.id"},
	} {
		t.Run(string(target.format), func(t *testing.T) {
			var req dto.OpenAIResponsesRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_output_tokens":64,"tools":[{"type":"custom","name":"exec","format":{"type":"text"}}],"tool_choice":{"type":"custom","name":"exec"},"input":[{"type":"custom_tool_call","call_id":"call_1","name":"exec","input":"echo \"ok\""},{"type":"custom_tool_call_output","call_id":"call_1","output":"ok"}]}`), &req))
			info := &convmeta.Values{}
			result, err := ConvertRequest(context.Background(), info, target.format, &req)
			require.NoError(t, err)
			encoded, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			assert.Equal(t, "exec", gjson.GetBytes(encoded, target.namePath).String())
			assert.JSONEq(t, `{"input":"echo \"ok\""}`, gjson.GetBytes(encoded, target.inputPath).String())
			assert.Equal(t, "exec", gjson.GetBytes(encoded, target.choicePath).String())
			assert.Equal(t, "call_1", gjson.GetBytes(encoded, target.resultPath).String())
			assert.True(t, info.ResponsesToolState().IsCustomTool("exec"))
			for _, diagnostic := range result.Diagnostics {
				assert.NotEqual(t, "retained_request_field_not_portable", diagnostic.Code, diagnostic.Path)
			}
			// Both a later ordinary conversion and a native retry must clear the record.
			_, err = ConvertRequest(context.Background(), info, target.format, &dto.OpenAIResponsesRequest{Model: "test", MaxOutputTokens: kitutil.GetPointer(uint(64)), Input: []byte(`"hi"`)})
			require.NoError(t, err)
			assert.Nil(t, info.ResponsesToolState())
			info.SetResponsesToolState(&convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}})
			requirements, err := AnalyzeRequest(&req)
			require.NoError(t, err)
			plan, err := PlanConversion(requirements, types.RelayFormatOpenAIResponses, types.ConversionLossPolicySafe)
			require.NoError(t, err)
			_, err = ExecuteConversionPlan(context.Background(), info, plan, &req)
			require.NoError(t, err)
			assert.Nil(t, info.ResponsesToolState())
			info.SetResponsesToolState(&convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}})
			_, err = ConvertRequest(context.Background(), info, types.RelayFormatOpenAIResponses, &req)
			require.NoError(t, err)
			assert.Nil(t, info.ResponsesToolState())
		})
	}
}

func TestCustomGrammarRemainsAnExplicitConversionLoss(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_output_tokens":64,"input":"hi","tools":[{"type":"custom","name":"exec","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}]}`), &req))
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
		_, err := ConvertRequest(context.Background(), nil, target, &req)
		require.ErrorContains(t, err, "tools[0].format")
		result, err := ConvertRequest(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}, target, &req)
		require.NoError(t, err)
		found := false
		for _, diagnostic := range result.Diagnostics {
			found = found || diagnostic.Code == "custom_tool_grammar_not_portable"
		}
		assert.True(t, found)
	}
}

func TestClaudeSafeguardsRemainNativeAndCannotSilentlyDisappear(t *testing.T) {
	for _, safeguards := range []string{`null`, `{}`, `{"auto_mode":true}`} {
		body := []byte(`{"model":"claude-test","max_tokens":64,"messages":[{"role":"user","content":"hi"}],"safeguards":` + safeguards + `,"output_config":{"effort":"high"}}`)
		var req dto.ClaudeRequest
		require.NoError(t, kitutil.Unmarshal(body, &req))
		native, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, &req)
		require.NoError(t, err)
		encoded, err := kitutil.Marshal(native.Value)
		require.NoError(t, err)
		assert.Equal(t, safeguards, gjson.GetBytes(encoded, "safeguards").Raw)
		assert.Equal(t, "high", gjson.GetBytes(encoded, "output_config.effort").String())
		for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
			_, err := ConvertRequest(context.Background(), nil, target, &req)
			require.ErrorContains(t, err, "safeguards")
			result, err := ConvertRequest(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}, target, &req)
			require.NoError(t, err)
			found := false
			for _, diagnostic := range result.Diagnostics {
				found = found || diagnostic.Code == "claude_safeguards_not_portable"
			}
			assert.True(t, found)
		}
	}
}

func TestClaudeHostedAndCustomToolsKeepDistinctOutputKinds(t *testing.T) {
	var response dto.ClaudeResponse
	require.NoError(t, kitutil.Unmarshal([]byte(`{"stop_reason":"tool_use","content":[{"type":"server_tool_use","id":"srv","name":"web_search","input":{"query":"test"}},{"type":"web_search_tool_result","tool_use_id":"srv","content":[]},{"type":"tool_use","id":"call_exec","name":"exec","input":{"input":"ls"}}]}`), &response))
	info := &convmeta.Values{ResponsesTools: &convmeta.ResponsesToolState{CustomToolNames: map[string]struct{}{"exec": {}}}}
	result, err := ConvertResponse(context.Background(), info, types.RelayFormatOpenAIResponses, &response)
	require.NoError(t, err)
	converted := result.Value.(*dto.OpenAIResponsesResponse)
	require.Len(t, converted.Output, 2)
	assert.Equal(t, "web_search_call", converted.Output[0].Type)
	assert.Equal(t, "custom_tool_call", converted.Output[1].Type)
	assert.Equal(t, `"ls"`, string(converted.Output[1].Input))
}
