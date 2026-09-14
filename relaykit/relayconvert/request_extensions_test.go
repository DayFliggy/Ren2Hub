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
)

func TestRequestExtensionsRemainVisibleInPlanningAndExecution(t *testing.T) {
	for _, test := range []struct {
		name    string
		request any
		body    string
		target  types.RelayFormat
	}{
		{"chat", &dto.GeneralOpenAIRequest{}, `{"model":"test","temperature":0,"messages":[{"role":"user","content":"hello"}],"extension_zero":0,"extension_false":false,"extension_null":null}`, types.RelayFormatGemini},
		{"responses", &dto.OpenAIResponsesRequest{}, `{"model":"test","input":"hello","extension_zero":0,"extension_false":false,"extension_null":null}`, types.RelayFormatGemini},
		{"claude", &dto.ClaudeRequest{}, `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"extension_zero":0,"extension_false":false,"extension_null":null}`, types.RelayFormatGemini},
		{"gemini", &dto.GeminiChatRequest{}, `{"system_instruction":{"parts":[{"text":"rule"}]},"contents":[{"role":"user","parts":[{"text":"hello"}]}],"extension_zero":0,"extension_false":false,"extension_null":null}`, types.RelayFormatOpenAI},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequestJSON(test.request, []byte(test.body))
			require.NoError(t, err)
			require.Len(t, requirements.Diagnostics, 3)
			assert.Equal(t, "extension_false", requirements.Diagnostics[0].Path)
			assert.Equal(t, "extension_null", requirements.Diagnostics[1].Path)
			assert.Equal(t, "extension_zero", requirements.Diagnostics[2].Path)
			encoded, err := kitutil.Marshal(requirements)
			require.NoError(t, err)
			assert.NotContains(t, string(encoded), "hello")
			for _, target := range []types.RelayFormat{requirements.Format, test.target} {
				strict, err := PlanConversion(requirements, target, types.ConversionLossPolicyStrict)
				require.ErrorContains(t, err, "extension_false")
				_, err = ExecuteConversionPlan(context.Background(), nil, strict, test.request)
				require.Error(t, err, "a rejected plan cannot be executed after DTO decoding discarded the field")
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow} {
					plan, err := PlanConversion(requirements, target, policy)
					require.NoError(t, err)
					meta := &convmeta.Values{UpstreamModelName: "test", ChannelMetaAttached: true}
					result, err := ExecuteConversionPlan(context.Background(), meta, plan, test.request)
					require.NoError(t, err)
					assert.Subset(t, result.Diagnostics, plan.Diagnostics)
				}
			}
		})
	}
}

func TestResponsesInstructionsInvalidBeforePlanning(t *testing.T) {
	for _, raw := range []string{`false`, `0`, `{}`, `[]`} {
		_, err := AnalyzeRequest(&dto.OpenAIResponsesRequest{Instructions: []byte(raw)})
		assert.ErrorContains(t, err, "instructions must be a string", raw)
	}
}

func TestNestedDTOExtensionsRejectStrictBeforeConversion(t *testing.T) {
	for _, test := range []struct {
		name       string
		request    any
		body, path string
	}{
		{"chat", &dto.GeneralOpenAIRequest{}, `{"model":"test","messages":[{"role":"user","content":"hello","vendor":false}]}`, "messages[0].vendor"},
		{"responses", &dto.OpenAIResponsesRequest{}, `{"model":"test","input":"hello","stream_options":{"vendor":0}}`, "stream_options.vendor"},
		{"claude", &dto.ClaudeRequest{}, `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello","vendor":null}]}`, "messages[0].vendor"},
		{"gemini", &dto.GeminiChatRequest{}, `{"contents":[{"parts":[{"text":"hello","vendor":false}]}]}`, "contents[0].parts[0].vendor"},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequestJSON(test.request, []byte(test.body))
			require.NoError(t, err)
			require.Len(t, requirements.Diagnostics, 1)
			assert.Equal(t, test.path, requirements.Diagnostics[0].Path)
			_, err = PlanConversion(requirements, requirements.Format, types.ConversionLossPolicyStrict)
			require.ErrorContains(t, err, test.path)
		})
	}
}

func TestExtensionInspectionPreservesOpenSchemasAndGeminiAliases(t *testing.T) {
	for _, test := range []struct {
		request any
		body    string
	}{
		{&dto.GeneralOpenAIRequest{}, `{"messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"example","parameters":{"type":"object","properties":{"arbitrary":{"type":"boolean","default":false}}}}}],"response_format":{"type":"json_schema","json_schema":{"name":"example","schema":{"properties":{"arbitrary":{"type":"number"}}}}}}`},
		{&dto.GeminiChatRequest{}, `{"system_instruction":{"parts":[{"text":"rule"}]},"contents":[{"parts":[{"text":"hello"},{"inline_data":{"mime_type":"image/png","data":"AA=="}},{"functionCall":{"name":"example","args":{"arbitrary":false}}}]}],"generationConfig":{"top_p":0,"top_k":0,"max_output_tokens":50,"candidate_count":1,"response_json_schema":{"properties":{"arbitrary":{"type":"number"}}},"thinking_config":{"include_thoughts":false,"thinking_budget":0}}}`},
		{&dto.OpenAIResponsesRequest{}, `{"input":[{"type":"function_call_output","call_id":"call_one","output":"hello"}],"tools":[{"type":"function","name":"example","parameters":{"properties":{"arbitrary":{"type":"number"}}}}]}`},
	} {
		require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
		requirements, err := AnalyzeRequestJSON(test.request, []byte(test.body))
		require.NoError(t, err)
		for _, diagnostic := range requirements.Diagnostics {
			assert.NotEqual(t, "unknown_request_field", diagnostic.Code, diagnostic.Path)
		}
	}
}
