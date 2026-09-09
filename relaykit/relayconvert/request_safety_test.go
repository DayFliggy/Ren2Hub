package relayconvert

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestConversionPoliciesNeverDiscardNativeResponsesState(t *testing.T) {
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicyAllow, types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
		for _, raw := range []string{
			`{"model":"fixture","input":[{"type":"compaction_trigger"}]}`,
			`{"model":"fixture","input":[{"type":"compaction","encrypted_content":"opaque"}]}`,
			`{"model":"fixture","input":"continue","previous_response_id":"resp_bound"}`,
			`{"model":"fixture","input":"continue","conversation":{"id":"conv_bound"}}`,
			`{"model":"fixture","input":"continue","context_management":[{"type":"compaction"}]}`,
		} {
			t.Run(string(policy)+raw, func(t *testing.T) {
				var req dto.OpenAIResponsesRequest
				require.NoError(t, kitutil.Unmarshal([]byte(raw), &req))
				before, err := kitutil.Marshal(&req)
				require.NoError(t, err)
				info := &convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: policy}}
				for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude, types.RelayFormatGemini} {
					_, err = ConvertRequest(nil, info, target, &req)
					require.Error(t, err)
				}
				native, err := ConvertRequest(nil, info, types.RelayFormatOpenAIResponses, &req)
				require.NoError(t, err)
				after, err := kitutil.Marshal(native.Value)
				require.NoError(t, err)
				assert.JSONEq(t, string(before), string(after))
			})
		}
	}
}

func TestStrictPolicyIncludesReasoningAndSamplingDiagnostics(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{Model: "claude-opus-4-6", Messages: []dto.Message{{Role: "user", Content: "hello"}}, MaxTokens: kitutil.GetPointer(uint(4096)), ReasoningEffort: "high", Temperature: kitutil.GetPointer(0.5)}
	info := &convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyStrict}}
	result, err := ConvertRequest(nil, info, types.RelayFormatClaude, req)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Diagnostics)
	assert.Empty(t, info.ConversionChain)
	info.Options.ToolLossPolicy = types.ConversionLossPolicyAllow
	accepted, err := ConvertRequest(nil, info, types.RelayFormatClaude, req)
	require.NoError(t, err)
	require.NotEmpty(t, accepted.Diagnostics)
}

func TestSafePolicyRejectsClientFunctionToHostedAlias(t *testing.T) {
	req := &dto.GeneralOpenAIRequest{Model: "fixture", Messages: []dto.Message{{Role: "user", Content: "hello"}}, Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "googleSearch"}}}}
	info := &convmeta.Values{Options: &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe}}
	result, err := ConvertRequest(nil, info, types.RelayFormatGemini, req)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.NotNil(t, result)
	assert.Equal(t, "named_function_hosted_alias", result.Diagnostics[0].Code)
	info.Options.ToolLossPolicy = types.ConversionLossPolicyAllow
	result, err = ConvertRequest(nil, info, types.RelayFormatGemini, req)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Diagnostics)
}
