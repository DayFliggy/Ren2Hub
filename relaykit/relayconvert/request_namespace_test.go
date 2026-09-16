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

func TestResponsesNamespaceHistoryConversionPolicy(t *testing.T) {
	for _, test := range []struct {
		name, input, tools string
		paths              []string
	}{
		{
			name:  "history without declarations",
			input: `[{"type":"function_call","call_id":"call_docs","namespace":"docs","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_docs","output":"result"}]`,
			paths: []string{"input[0].namespace"},
		},
		{
			name:  "same name in distinct namespaces",
			input: `[{"type":"function_call","call_id":"call_docs","namespace":"docs","name":"read","arguments":"{}"},{"type":"function_call","call_id":"call_files","namespace":"files","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_docs","output":"docs result"},{"type":"function_call_output","call_id":"call_files","output":"files result"}]`,
			paths: []string{"input[0].namespace", "input[1].namespace"},
		},
		{
			name:  "declaration and history share policy",
			input: `[{"type":"function_call","call_id":"call_docs","namespace":"docs","name":"read","arguments":"{}"}]`,
			tools: `,"tools":[{"type":"namespace","name":"docs","tools":[{"type":"function","name":"read","parameters":{"type":"object"}}]}]`,
			paths: []string{"tools[0]", "input[0].namespace"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"test","max_output_tokens":64,"input":` + test.input + test.tools + `}`)
			var request dto.OpenAIResponsesRequest
			require.NoError(t, kitutil.Unmarshal(body, &request))
			requirements, err := AnalyzeRequestJSON(&request, body)
			require.NoError(t, err)
			for _, diagnostic := range requirements.Diagnostics {
				assert.NotEqual(t, "input[0].namespace", diagnostic.Path, "namespace identity must not be treated as an unknown extension")
			}
			for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
				for _, policy := range []types.ConversionLossPolicy{"", types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
					t.Run(fmt.Sprintf("%s/%s", target, policy), func(t *testing.T) {
						plan, err := PlanConversion(requirements, target, policy)
						if policy == types.ConversionLossPolicyAllow {
							require.NoError(t, err)
						} else {
							require.ErrorContains(t, err, "input[0].namespace")
						}
						for _, path := range test.paths {
							assert.Contains(t, plan.Diagnostics, types.ConversionDiagnostic{
								Code: "unsupported_tool_semantics", Path: path,
								Message:  "cannot preserve tool namespace and its configuration",
								Severity: types.ConversionDiagnosticError,
								From:     types.RelayFormatOpenAIResponses, To: target,
							})
						}
						if policy != types.ConversionLossPolicyAllow {
							_, err = ExecuteConversionPlan(context.Background(), nil, plan, &request)
							require.ErrorContains(t, err, "input[0].namespace")
						} else if test.tools == "" {
							result, err := ExecuteConversionPlan(context.Background(), nil, plan, &request)
							require.NoError(t, err)
							assert.Subset(t, result.Diagnostics, plan.Diagnostics, "explicit degradation must retain the identity loss diagnostic")
						}
					})
				}
			}
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				plan, err := PlanConversion(requirements, types.RelayFormatOpenAIResponses, policy)
				require.NoError(t, err)
				result, err := ExecuteConversionPlan(context.Background(), nil, plan, &request)
				require.NoError(t, err)
				assert.Same(t, &request, result.Value)
				encoded, err := kitutil.Marshal(result.Value)
				require.NoError(t, err)
				var forwarded dto.OpenAIResponsesRequest
				require.NoError(t, kitutil.Unmarshal(encoded, &forwarded))
				assert.JSONEq(t, test.input, string(forwarded.Input))
			}
		})
	}
}

func TestResponsesNamespaceHistoryPublicConversionsDefaultSafe(t *testing.T) {
	var request dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_output_tokens":64,"input":[{"type":"function_call","call_id":"call_docs","namespace":"docs","name":"read","arguments":"{}"}]}`), &request))
	_, err := ResponsesRequestToChatCompletionsRequest(&request)
	require.ErrorContains(t, err, "input[0].namespace")
	_, err = OpenAIResponsesRequestToClaudeMessages(context.Background(), nil, &request)
	require.ErrorContains(t, err, "input[0].namespace")
	_, err = OpenAIResponsesRequestToGeminiChat(context.Background(), &request, nil)
	require.ErrorContains(t, err, "input[0].namespace")
}

func TestResponsesNamespaceHistoryValidation(t *testing.T) {
	for _, value := range []string{`""`, `"  "`, `0`, `false`, `[]`, `{}`} {
		t.Run(value, func(t *testing.T) {
			var request dto.OpenAIResponsesRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","input":[{"type":"function_call","call_id":"call_docs","namespace":`+value+`,"name":"read","arguments":"{}"}]}`), &request))
			_, err := AnalyzeRequest(&request)
			require.ErrorContains(t, err, "input[0].namespace must be a non-empty string or null")
		})
	}
	for _, namespace := range []string{"", `,"namespace":null`} {
		var request dto.OpenAIResponsesRequest
		require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_output_tokens":64,"input":[{"type":"function_call","call_id":"call_docs","name":"read","arguments":"{}"`+namespace+`}]}`), &request))
		requirements, err := AnalyzeRequest(&request)
		require.NoError(t, err)
		for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
			_, err := PlanConversion(requirements, target, types.ConversionLossPolicyStrict)
			require.NoError(t, err, "missing or null namespace has no namespace identity to lose")
		}
	}
}
