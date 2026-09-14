package relayconvert

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetainedProtocolExtensionsRequireStrictRejectionOnlyWhenConverting(t *testing.T) {
	for _, test := range []struct {
		name, body, path string
		request          any
		target           types.RelayFormat
	}{
		{"chat content", `{"model":"test","messages":[{"role":"user","content":[{"type":"text","text":"private prompt","vendor":false}]}]}`, "messages[0].content[0].vendor", &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
		{"chat media", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://private.invalid","vendor":null}}]}]}`, "messages[0].content[0].image_url.vendor", &dto.GeneralOpenAIRequest{}, types.RelayFormatClaude},
		{"responses input", `{"model":"test","input":[{"role":"user","content":[{"type":"input_text","text":"private prompt","vendor":0}]}]}`, "input[0].content[0].vendor", &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
		{"responses tools", `{"model":"test","input":"hi","tools":[{"type":"function","name":"f","parameters":{},"vendor":false}]}`, "tools[0].vendor", &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
		{"responses choice", `{"model":"test","input":"hi","tool_choice":{"type":"function","name":"f","vendor":false}}`, "tool_choice.vendor", &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
		{"claude content", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":[{"type":"text","text":"private prompt","vendor":0}]}]}`, "messages[0].content[0].vendor", &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"claude tools", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"f","input_schema":{},"vendor":null}]}`, "tools[0].vendor", &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"gemini declarations", `{"contents":[{"parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":"f","parameters":{},"vendor":null}]}]}`, "tools[0].functionDeclarations[0].vendor", &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequestJSON(test.request, []byte(test.body))
			require.NoError(t, err)
			require.Len(t, requirements.Diagnostics, 1)
			assert.Equal(t, "unknown_conversion_field", requirements.Diagnostics[0].Code)
			assert.Equal(t, test.path, requirements.Diagnostics[0].Path)
			encoded, err := kitutil.Marshal(requirements)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "private prompt")
			assert.NotContains(t, string(encoded), "private.invalid")
			_, err = PlanConversion(requirements, requirements.Format, types.ConversionLossPolicyStrict)
			require.NoError(t, err, "native forwarding retains the raw envelope")
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow} {
				_, err = PlanConversion(requirements, test.target, policy)
				require.NoError(t, err)
			}
			_, err = PlanConversion(requirements, test.target, types.ConversionLossPolicyStrict)
			require.ErrorContains(t, err, test.path)
			_, err = ConvertRequest(context.Background(), nil, test.target, test.request)
			require.NoError(t, err, "safe compatibility entry keeps unknown extension diagnostics")
		})
	}
}

func TestToolSchemaProjectionRejectsDroppedConstraints(t *testing.T) {
	for _, test := range []struct {
		name, schema, path string
	}{
		{"constant", `{"type":"object","properties":{"mode":{"type":"string","const":"private value"}}}`, "tools[0].function.parameters.properties.mode.const"},
		{"numeric multiple", `{"type":"object","properties":{"amount":{"type":"integer","multipleOf":2}}}`, "tools[0].function.parameters.properties.amount.multipleOf"},
		{"type union", `{"type":"object","properties":{"value":{"type":["string","integer"]}}}`, "tools[0].function.parameters.properties.value.type"},
		{"tuple", `{"type":"object","properties":{"value":{"type":"array","items":[{"type":"string"},{"type":"integer"}]}}}`, "tools[0].function.parameters.properties.value.items"},
		{"nested additional", `{"type":"object","properties":{"value":{"type":"object","additionalProperties":false}}}`, "tools[0].function.parameters.properties.value.additionalProperties"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"f","parameters":`+test.schema+`}}]}`), &request))
			before, err := kitutil.Marshal(request)
			require.NoError(t, err)
			requirements, err := AnalyzeRequest(&request)
			require.NoError(t, err)
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
				_, err = PlanConversion(requirements, types.RelayFormatGemini, policy)
				require.ErrorContains(t, err, test.path)
			}
			plan, err := PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicyAllow)
			require.NoError(t, err)
			result, err := ExecuteConversionPlan(context.Background(), nil, plan, &request)
			require.NoError(t, err)
			assert.NotEmpty(t, result.Diagnostics)
			_, err = PlanConversion(requirements, types.RelayFormatClaude, types.ConversionLossPolicyStrict)
			require.NoError(t, err, "Claude path preserves the original JSON Schema")
			after, err := kitutil.Marshal(request)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after))
		})
	}
}

func TestStructuredOutputAndToolExecutionLossCannotUseSafePolicy(t *testing.T) {
	for _, test := range []struct {
		name, body, path string
		request          any
		target           types.RelayFormat
	}{
		{"chat schema", `{"model":"test","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object","additionalProperties":false}}}}`, "response_format.json_schema.schema.additionalProperties", &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
		{"chat json", `{"model":"test","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"}}`, "response_format", &dto.GeneralOpenAIRequest{}, types.RelayFormatClaude},
		{"responses strict", `{"model":"test","input":"hi","text":{"format":{"type":"json_schema","name":"result","strict":true,"schema":{"type":"object"}}}}`, "text.format.strict", &dto.OpenAIResponsesRequest{}, types.RelayFormatGemini},
		{"claude output", `{"model":"test","messages":[{"role":"user","content":"hi"}],"output_config":{"format":{"type":"json_schema","schema":{"type":"object"}}}}`, "output_config.format", &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"gemini output", `{"contents":[{"parts":[{"text":"hi"}]}],"generationConfig":{"responseMimeType":"application/json","responseJsonSchema":{"type":"object"}}}`, "generationConfig.responseJsonSchema", &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"claude failed tool", `{"model":"test","messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_f","content":"error","is_error":true}]}]}`, "messages[0].content[0].is_error", &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"gemini tool continuation", `{"contents":[{"parts":[{"functionResponse":{"name":"f","response":{},"willContinue":true}}]}]}`, "contents[0].parts[0].functionResponse.willContinue", &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"gemini tool schema", `{"contents":[{"parts":[{"text":"hi"}]}],"tools":[{"functionDeclarations":[{"name":"f","parametersJsonSchema":{"type":"object","additionalProperties":false}}]}]}`, "tools[0].functionDeclarations[0].parametersJsonSchema", &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"claude serial tools", `{"model":"test","messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"auto","disable_parallel_tool_use":true}}`, "parallel_tool_calls", &dto.ClaudeRequest{}, types.RelayFormatGemini},
		{"responses restricted callers", `{"model":"test","input":"hi","tools":[{"type":"function","name":"f","allowed_callers":["code_execution"]}]}`, "tools[0].allowed_callers", &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
		{"gemini validated tool choice", `{"contents":[{"parts":[{"text":"hi"}]}],"toolConfig":{"functionCallingConfig":{"mode":"VALIDATED","allowedFunctionNames":["f"]}}}`, "toolConfig.functionCallingConfig.mode", &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"chat allowed tool choice", `{"model":"test","messages":[{"role":"user","content":"hi"}],"tool_choice":{"type":"allowed_tools","mode":"required","tools":[{"type":"function","function":{"name":"f"}}]}}`, "tool_choice.type", &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			_, err = PlanConversion(requirements, requirements.Format, types.ConversionLossPolicyStrict)
			require.NoError(t, err)
			_, err = PlanConversion(requirements, test.target, types.ConversionLossPolicySafe)
			require.ErrorContains(t, err, test.path)
			_, err = PlanConversion(requirements, test.target, types.ConversionLossPolicyAllow)
			require.NoError(t, err)
		})
	}
}

func TestDiscardedToolExecutionControlsRejectSafeNativeForwarding(t *testing.T) {
	for _, field := range []string{"allowed_callers", "defer_loading"} {
		t.Run(field, func(t *testing.T) {
			body := []byte(`{"model":"test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"f"},"` + field + `":false}]}`)
			var request dto.GeneralOpenAIRequest
			require.NoError(t, kitutil.Unmarshal(body, &request))
			requirements, err := AnalyzeRequestJSON(&request, body)
			require.NoError(t, err)
			for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatGemini} {
				_, err = PlanConversion(requirements, target, types.ConversionLossPolicySafe)
				require.ErrorContains(t, err, field)
				_, err = PlanConversion(requirements, target, types.ConversionLossPolicyAllow)
				require.NoError(t, err)
			}
		})
	}
}

func TestProtocolInspectionKeepsBusinessPayloadsOpen(t *testing.T) {
	for _, test := range []struct {
		request any
		body    string
		target  types.RelayFormat
	}{
		{&dto.GeneralOpenAIRequest{}, `{"model":"test","messages":[{"role":"assistant","tool_calls":[{"id":"call_f","type":"function","function":{"name":"f","arguments":"{\"signature\":\"business data\",\"type\":\"compaction\"}"}}]},{"role":"tool","tool_call_id":"call_f","content":"result"}],"tools":[{"type":"function","function":{"name":"f","parameters":{"type":"object","properties":{"additionalProperties":{"type":["string","null"]},"signature":{"type":"string"},"const":{"type":"boolean"}}}}}]}`, types.RelayFormatGemini},
		{&dto.ClaudeRequest{}, `{"model":"test","messages":[{"role":"assistant","content":[{"type":"tool_use","id":"call_f","name":"f","input":{"signature":"business data","type":"compaction","nested":{"vendor":false}}}]}]}`, types.RelayFormatOpenAI},
		{&dto.GeminiChatRequest{}, `{"contents":[{"parts":[{"functionCall":{"name":"f","args":{"signature":"business data","type":"compaction"}}}]}],"tools":[{"functionDeclarations":[{"name":"f","parameters":{"type":"object","properties":{"vendor":{"type":"boolean"}}}}]}]}`, types.RelayFormatOpenAI},
	} {
		require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
		requirements, err := AnalyzeRequestJSON(test.request, []byte(test.body))
		require.NoError(t, err)
		assert.False(t, requirements.NativeRequired)
		_, err = PlanConversion(requirements, test.target, types.ConversionLossPolicyStrict)
		require.NoError(t, err)
	}
}

func TestSchemaDiagnosticExpansionIsBounded(t *testing.T) {
	properties := make(map[string]any)
	for index := 0; index < 200; index++ {
		properties[fmt.Sprintf("field_%03d", index)] = map[string]any{"type": "integer", "multipleOf": 2}
	}
	request := &dto.GeneralOpenAIRequest{Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "f", Parameters: map[string]any{"type": "object", "properties": properties}}}}}
	_, err := AnalyzeRequest(request)
	require.ErrorContains(t, err, "diagnostic limits")
}

func TestExplicitFalseStrictnessRemainsDistinguishableFromMissing(t *testing.T) {
	for _, body := range []string{
		`{"model":"test","messages":[{"role":"user","content":"hi"}],"tools":[{"type":"function","function":{"name":"f","strict":false,"parameters":{"type":"object"}}}]}`,
		`{"model":"test","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_schema","json_schema":{"name":"result","strict":false,"schema":{"type":"object"}}}}`,
	} {
		var request dto.GeneralOpenAIRequest
		require.NoError(t, kitutil.Unmarshal([]byte(body), &request))
		requirements, err := AnalyzeRequest(&request)
		require.NoError(t, err)
		_, err = PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicySafe)
		require.NoError(t, err)
		_, err = PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicyStrict)
		require.ErrorContains(t, err, ".strict")
	}
}
