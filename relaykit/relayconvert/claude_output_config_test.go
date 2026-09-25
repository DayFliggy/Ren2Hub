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

func TestClaudeMessageOutputConfigConversionPolicy(t *testing.T) {
	for _, topLevel := range []string{"", `,"output_config":{"effort":"medium"}`} {
		for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				t.Run(fmt.Sprintf("top=%t/%s/%s", topLevel != "", target, policy), func(t *testing.T) {
					body := []byte(`{"model":"test","max_tokens":64` + topLevel + `,"messages":[{"role":"user","content":"private history"},{"role":"assistant","content":"done"},{"role":"system","content":[],"output_config":{"effort":"high"}},{"role":"user","content":"continue"}]}`)
					var request dto.ClaudeRequest
					require.NoError(t, kitutil.Unmarshal(body, &request))
					before, err := kitutil.Marshal(request)
					require.NoError(t, err)
					requirements, err := AnalyzeRequestJSON(&request, body)
					require.NoError(t, err)
					plan, planErr := PlanConversion(requirements, target, policy)
					result, executionErr := ExecuteConversionPlan(context.Background(), nil, plan, &request)
					require.NotNil(t, result)
					if target == types.RelayFormatClaude {
						require.NoError(t, planErr)
						require.NoError(t, executionErr)
						assert.Same(t, &request, result.Value)
						assert.Empty(t, plan.Diagnostics)
						assert.Empty(t, result.Diagnostics)
					} else {
						diagnostic := types.ConversionDiagnostic{
							Code: "claude_message_output_config_not_portable", Path: "messages[2].output_config",
							Message:  "conversion cannot preserve Claude per-message output configuration",
							Severity: types.ConversionDiagnosticError, From: types.RelayFormatClaude, To: target,
						}
						assert.Contains(t, plan.Diagnostics, diagnostic)
						assert.Contains(t, result.Diagnostics, diagnostic)
						if policy == types.ConversionLossPolicyAllow {
							require.NoError(t, planErr)
							require.NoError(t, executionErr)
							encoded, err := kitutil.Marshal(result.Value)
							require.NoError(t, err)
							assert.NotContains(t, string(encoded), `"output_config"`)
							assert.NotContains(t, string(encoded), `"high"`, "the message effort must not become global effort")
						} else {
							require.ErrorContains(t, planErr, "messages[2].output_config")
							require.ErrorContains(t, executionErr, "messages[2].output_config")
							assert.Nil(t, result.Value)
						}
					}
					diagnostics, err := kitutil.Marshal(result.Diagnostics)
					require.NoError(t, err)
					assert.NotContains(t, string(diagnostics), "private history")
					assert.NotContains(t, string(diagnostics), "high")
					after, err := kitutil.Marshal(request)
					require.NoError(t, err)
					assert.JSONEq(t, string(before), string(after), "conversion must not modify the source request")
				})
			}
		}
	}
}

func TestClaudeMessageOutputConfigPresenceAndOpaquePayload(t *testing.T) {
	for _, config := range []string{`null`, `{}`, `{"future":{"value":"private config","enabled":false},"effort":"future-effort"}`} {
		t.Run(config, func(t *testing.T) {
			body := []byte(`{"model":"test","max_tokens":64,"messages":[{"role":"system","content":[],"output_config":` + config + `},{"role":"user","content":"hello"}]}`)
			var request dto.ClaudeRequest
			require.NoError(t, kitutil.Unmarshal(body, &request))
			for _, analyze := range []func() (RequestRequirements, error){
				func() (RequestRequirements, error) { return AnalyzeRequest(&request) },
				func() (RequestRequirements, error) { return AnalyzeRequestJSON(&request, body) },
			} {
				requirements, err := analyze()
				require.NoError(t, err)
				require.Len(t, requirements.Diagnostics, 1)
				assert.Equal(t, "messages[0].output_config", requirements.Diagnostics[0].Path)
				assert.Equal(t, "claude_message_output_config_not_portable", requirements.Diagnostics[0].Code)
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
					plan, err := PlanConversion(requirements, types.RelayFormatClaude, policy)
					require.NoError(t, err)
					result, err := ExecuteConversionPlan(context.Background(), nil, plan, &request)
					require.NoError(t, err)
					forwarded, ok := result.Value.(*dto.ClaudeRequest)
					require.True(t, ok)
					assert.JSONEq(t, config, string(forwarded.Messages[0].OutputConfig))
				}
				for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatGemini} {
					_, err := PlanConversion(requirements, target, "")
					require.ErrorContains(t, err, "messages[0].output_config", "default safe must detect presence, including null and empty objects")
				}
				encoded, err := kitutil.Marshal(requirements.Diagnostics)
				require.NoError(t, err)
				assert.NotContains(t, string(encoded), "private config")
				assert.NotContains(t, string(encoded), "future-effort")
			}
		})
	}
}

func TestClaudeMessageOutputConfigPublicEntryPoints(t *testing.T) {
	var request dto.ClaudeRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_tokens":64,"messages":[{"role":"system","content":[],"output_config":{"effort":"high"}},{"role":"user","content":"hello"}]}`), &request))
	for _, entry := range []struct {
		name    string
		convert func(convmeta.Meta) (*RequestResult, error)
	}{
		{"automatic", func(meta convmeta.Meta) (*RequestResult, error) {
			return ConvertRequest(context.Background(), meta, types.RelayFormatOpenAI, &request)
		}},
		{"by ID", func(meta convmeta.Meta) (*RequestResult, error) {
			return ConvertRequestByID(context.Background(), meta, ConverterClaudeMessagesToOpenAIChat, &request)
		}},
		{"via chat", func(meta convmeta.Meta) (*RequestResult, error) {
			return ConvertRequestVia(context.Background(), meta, &request, types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses)
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			_, err := entry.convert(nil)
			require.ErrorContains(t, err, "messages[0].output_config")
			result, err := entry.convert(&convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}})
			require.NoError(t, err)
			require.NotEmpty(t, result.Diagnostics)
			assert.Equal(t, "claude_message_output_config_not_portable", result.Diagnostics[0].Code)
		})
	}
	_, err := ClaudeMessagesRequestToOpenAIChat(request, nil)
	require.ErrorContains(t, err, "messages[0].output_config")

	clean := request
	clean.Messages = append([]dto.ClaudeMessage(nil), request.Messages...)
	clean.Messages[0].OutputConfig = nil
	requirements, err := AnalyzeRequest(&clean)
	require.NoError(t, err)
	plan, err := PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
	_, err = ExecuteConversionPlan(context.Background(), nil, plan, &request)
	require.ErrorContains(t, err, "messages[0].output_config", "adding message configuration after planning cannot bypass the selected policy")
}

func TestClaudeMessageOutputConfigRoundTrip(t *testing.T) {
	for _, config := range []string{"", `,"output_config":{"effort":"high"}`} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
			t.Run(fmt.Sprintf("configured=%t/%s", config != "", policy), func(t *testing.T) {
				var request dto.ClaudeRequest
				require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_tokens":64,"messages":[{"role":"system","content":[]`+config+`},{"role":"user","content":"hello"}]}`), &request))
				before, err := kitutil.Marshal(request)
				require.NoError(t, err)
				result, err := ConvertRequestVia(context.Background(), &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}}, &request, types.RelayFormatOpenAI, types.RelayFormatClaude)
				require.NotNil(t, result)
				assert.EqualValues(t, types.RelayFormatClaude, result.From)
				assert.EqualValues(t, types.RelayFormatClaude, result.To)
				if config != "" {
					assert.Contains(t, result.Diagnostics, types.ConversionDiagnostic{
						Code: "claude_message_output_config_not_portable", Path: "messages[0].output_config",
						Message:  "conversion cannot preserve Claude per-message output configuration",
						Severity: types.ConversionDiagnosticError, From: types.RelayFormatClaude, To: types.RelayFormatOpenAI,
					})
					if policy != types.ConversionLossPolicyAllow {
						require.ErrorContains(t, err, "messages[0].output_config")
						assert.Nil(t, result.Value)
						assert.Empty(t, result.Steps, "reject before the first lossy conversion")
						return
					}
				}
				require.NoError(t, err)
				require.Len(t, result.Steps, 2)
				forwarded, ok := result.Value.(*dto.ClaudeRequest)
				require.True(t, ok)
				for _, message := range forwarded.Messages {
					assert.Empty(t, message.OutputConfig)
				}
				after, err := kitutil.Marshal(request)
				require.NoError(t, err)
				assert.JSONEq(t, string(before), string(after))
			})
		}
	}
}
