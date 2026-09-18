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

func TestToolMediaConversionPreservesInterleavedHistory(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"fixture","max_tokens":100,"messages":[{"role":"user","content":"start"},{"role":"assistant","content":[{"type":"text","text":"before call"},{"type":"tool_use","id":"call_image","name":"screenshot","input":{}},{"type":"text","text":"after call"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_image","content":[{"type":"text","text":"before image"},{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}},{"type":"text","text":"after image"}]},{"type":"text","text":"after result"}]}]}`), &request))
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, &request)
	require.NoError(t, err)
	converted := result.Value.(*dto.OpenAIResponsesRequest)
	assert.JSONEq(t, `[{"role":"user","content":[{"type":"input_text","text":"start"}]},{"role":"assistant","content":[{"type":"output_text","text":"before call"}]},{"type":"function_call","call_id":"call_image","name":"screenshot","arguments":"{}"},{"role":"assistant","content":[{"type":"output_text","text":"after call"}]},{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_text","text":"before image"},{"type":"input_image","image_url":"https://example.test/image.png"},{"type":"input_text","text":"after image"}]},{"role":"user","content":[{"type":"input_text","text":"after result"}]}]`, string(converted.Input))
	back, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, converted)
	require.NoError(t, err)
	encoded, err := kitutil.Marshal(back.Value.(*dto.ClaudeRequest).Messages)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"role":"user","content":[{"type":"text","text":"start"}]},{"role":"assistant","content":[{"type":"text","text":"before call"},{"type":"tool_use","id":"call_image","name":"screenshot","input":{}},{"type":"text","text":"after call"}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_image","content":[{"type":"text","text":"before image"},{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}},{"type":"text","text":"after image"}]},{"type":"text","text":"after result"}]}]`, string(encoded))
}

func TestToolMediaConversionKeepsTextResultsAndDoesNotResolveMedia(t *testing.T) {
	SetMediaResolver(MediaResolver{GetBase64Data: func(context.Context, types.FileSource, ...string) (string, string, error) {
		t.Fatal("structured tool media conversion must not download or resolve image bytes")
		return "", "", nil
	}})
	t.Cleanup(func() { SetMediaResolver(goldenMediaResolver()) })
	var request dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"fixture","max_output_tokens":100,"temperature":0,"parallel_tool_calls":false,"input":[{"role":"user","content":"start"},{"type":"function_call","call_id":"text_call","name":"text_tool","arguments":"{}"},{"type":"function_call","call_id":"image_call","name":"image_tool","arguments":"{}"},{"type":"function_call_output","call_id":"text_call","output":"text result"},{"type":"function_call_output","call_id":"image_call","output":[{"type":"input_text","text":"before"},{"type":"input_image","image_url":"data:image/png;base64,AQID"},{"type":"input_text","text":"after"}]}]}`), &request))
	original, err := kitutil.Marshal(request)
	require.NoError(t, err)
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, &request)
	require.NoError(t, err)
	converted := result.Value.(*dto.ClaudeRequest)
	require.NotNil(t, converted.Temperature)
	assert.Equal(t, float64(0), *converted.Temperature)
	encoded, err := kitutil.Marshal(converted)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"disable_parallel_tool_use":true`)
	messageJSON, err := kitutil.Marshal(converted.Messages[2])
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"user","content":[{"type":"tool_result","tool_use_id":"text_call","content":"text result"},{"type":"tool_result","tool_use_id":"image_call","content":[{"type":"text","text":"before"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AQID"}},{"type":"text","text":"after"}]}]}`, string(messageJSON))
	after, err := kitutil.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, string(original), string(after), "conversion must leave the original available for other candidates")
}

func TestToolMediaPlanRejectsKnownUnrepresentableHistory(t *testing.T) {
	for _, test := range []struct {
		name, body, code string
		request          any
		target           types.RelayFormat
	}{
		{"reused identity", `{"model":"fixture","max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"same","content":[{"type":"image","source":{"type":"url","url":"https://example.test/first.png"}}]},{"type":"tool_result","tool_use_id":"same","content":[{"type":"image","source":{"type":"url","url":"https://example.test/second.png"}}]}]}]}`, "tool_result_identity_ambiguous", &dto.ClaudeRequest{}, types.RelayFormatOpenAIResponses},
		{"text and image share identity", `{"model":"fixture","max_output_tokens":100,"input":[{"type":"function_call_output","call_id":"same","output":"text result"},{"type":"function_call_output","call_id":"same","output":[{"type":"input_image","image_url":"https://example.test/image.png"}]}]}`, "tool_result_identity_ambiguous", &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
		{"reused call declaration", `{"model":"fixture","max_output_tokens":100,"input":[{"type":"function_call","call_id":"same","name":"first","arguments":"{}"},{"type":"function_call","call_id":"same","name":"second","arguments":"{}"},{"type":"function_call_output","call_id":"same","output":[{"type":"input_image","image_url":"https://example.test/image.png"}]}]}`, "tool_result_identity_ambiguous", &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
		{"missing identity", `{"model":"fixture","max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_result","content":[{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}]}]}]}`, "tool_result_identity_missing", &dto.ClaudeRequest{}, types.RelayFormatOpenAIResponses},
		{"failed tool", `{"model":"fixture","max_tokens":100,"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_image","is_error":true,"content":[{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}]}]}]}`, "tool_result_status_not_portable", &dto.ClaudeRequest{}, types.RelayFormatOpenAIResponses},
		{"image detail", `{"model":"fixture","max_output_tokens":100,"input":[{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"https://example.test/image.png","detail":"high"}]}]}`, "tool_media_detail_not_portable", &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
		{"nonbase64 data URL", `{"model":"fixture","max_output_tokens":100,"input":[{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"data:image/png,invalid"}]}]}`, "tool_media_content_unsupported", &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
		{"mixed unsupported part", `{"model":"fixture","max_output_tokens":100,"input":[{"type":"function_call_output","call_id":"call_image","output":[{"type":"input_image","image_url":"https://example.test/image.png"},{"type":"unknown_content","value":"not portable"}]}]}`, "tool_media_content_unsupported", &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				plan, err := PlanConversion(requirements, test.target, policy)
				require.Error(t, err, "known conversion limits must reject during planning under %s", policy)
				var loss *types.ConversionLossError
				require.ErrorAs(t, err, &loss)
				codes := make([]string, 0, len(plan.Diagnostics))
				for _, diagnostic := range plan.Diagnostics {
					codes = append(codes, diagnostic.Code)
				}
				assert.Contains(t, codes, test.code)
				_, err = PlanConversion(requirements, requirements.Format, policy)
				require.NoError(t, err, "native requests are not governed by cross-protocol projection limits")
			}
		})
	}
}

func TestToolMediaPlanRejectsUnimplementedChatAndGeminiOrigins(t *testing.T) {
	for _, test := range []struct {
		name, body string
		request    any
	}{
		{"chat", `{"model":"fixture","messages":[{"role":"tool","tool_call_id":"call_image","content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}]}`, &dto.GeneralOpenAIRequest{}},
		{"gemini", `{"contents":[{"role":"user","parts":[{"functionResponse":{"name":"screenshot","id":"call_image","response":{},"parts":[{"inlineData":{"mimeType":"image/png","data":"AQID"}}]}}]}]}`, &dto.GeminiChatRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAIResponses} {
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyAllow} {
					_, err := PlanConversion(requirements, target, policy)
					require.Error(t, err, "%s tool media is not implemented for %s under %s", test.name, target, policy)
				}
			}
		})
	}
}
