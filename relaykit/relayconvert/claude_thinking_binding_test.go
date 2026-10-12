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

func TestClaudeThinkingControlsStayNativeAndDeclareConversionLoss(t *testing.T) {
	for _, test := range []struct {
		name, thinking, code, path string
		safeRejected               bool
	}{
		{"block binding", `{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"}}`, "claude_thinking_block_binding_not_portable", "thinking.block_binding", true},
		{"future zero", `{"type":"adaptive","future_field":0}`, "unknown_conversion_field", "thinking.future_field", false},
		{"future false", `{"type":"adaptive","future_field":false}`, "unknown_conversion_field", "thinking.future_field", false},
		{"future null", `{"type":"adaptive","future_field":null}`, "unknown_conversion_field", "thinking.future_field", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := []byte(`{"model":"claude-opus-5-5","max_tokens":128,"messages":[{"role":"user","content":"hi"}],"thinking":` + test.thinking + `}`)
			var request dto.ClaudeRequest
			require.NoError(t, kitutil.Unmarshal(body, &request))
			for _, rawAnalysis := range []bool{false, true} {
				var requirements RequestRequirements
				var err error
				if rawAnalysis {
					requirements, err = AnalyzeRequestJSON(&request, body)
				} else {
					requirements, err = AnalyzeRequest(&request)
				}
				require.NoError(t, err)
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
					native, err := PlanConversion(requirements, types.RelayFormatClaude, policy)
					require.NoError(t, err)
					assert.Empty(t, native.Diagnostics, "retained native fields are not losses")
					result, err := ExecuteConversionPlan(context.Background(), nil, native, &request)
					require.NoError(t, err)
					encoded, err := kitutil.Marshal(result.Value)
					require.NoError(t, err)
					assert.JSONEq(t, string(body), string(encoded))

					for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
						plan, planErr := PlanConversion(requirements, target, policy)
						var diagnostic *types.ConversionDiagnostic
						for index := range plan.Diagnostics {
							if plan.Diagnostics[index].Code == test.code && plan.Diagnostics[index].Path == test.path {
								diagnostic = &plan.Diagnostics[index]
							}
						}
						require.NotNil(t, diagnostic, "target=%s policy=%s", target, policy)
						meta := &convmeta.Values{UpstreamModelName: "test", ChannelMetaAttached: true}
						result, executionErr := ExecuteConversionPlan(context.Background(), meta, plan, &request)
						if policy == types.ConversionLossPolicyStrict || policy == types.ConversionLossPolicySafe && test.safeRejected {
							require.Error(t, planErr)
							require.Error(t, executionErr)
							continue
						}
						require.NoError(t, planErr)
						require.NoError(t, executionErr)
						assert.Contains(t, result.Diagnostics, *diagnostic)
						encoded, err := kitutil.Marshal(result.Value)
						require.NoError(t, err)
						assert.NotContains(t, string(encoded), "block_binding")
						assert.NotContains(t, string(encoded), "future_field")
					}
				}
			}
		})
	}
}
