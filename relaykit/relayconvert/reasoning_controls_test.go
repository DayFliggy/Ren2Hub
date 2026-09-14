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

func TestProviderReasoningControlsRequireExplicitLossPolicy(t *testing.T) {
	for _, test := range []struct {
		name, body, path string
		request          any
	}{
		{"claude enabled", `{"model":"test","max_tokens":2048,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":1024}}`, "thinking", &dto.ClaudeRequest{}},
		{"claude disabled", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"disabled"}}`, "thinking", &dto.ClaudeRequest{}},
		{"claude adaptive", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"adaptive"}}`, "thinking", &dto.ClaudeRequest{}},
		{"claude effort", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"output_config":{"effort":"low"}}`, "output_config.effort", &dto.ClaudeRequest{}},
		{"gemini disabled", `{"model":"test","contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0,"includeThoughts":false}}}`, "generationConfig.thinkingConfig", &dto.GeminiChatRequest{}},
		{"gemini budget alias", `{"model":"test","contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinking_config":{"thinking_budget":1024,"include_thoughts":true}}}`, "generationConfig.thinkingConfig", &dto.GeminiChatRequest{}},
		{"gemini effort", `{"model":"test","contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingLevel":"low"}}}`, "generationConfig.thinkingConfig", &dto.GeminiChatRequest{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			assert.Contains(t, requirements.Features, "reasoning")
			_, err = PlanConversion(requirements, requirements.Format, types.ConversionLossPolicyStrict)
			require.NoError(t, err, "native controls are retained")
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
				_, err = PlanConversion(requirements, types.RelayFormatOpenAI, policy)
				require.ErrorContains(t, err, test.path)
			}
			_, err = ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, test.request)
			require.ErrorContains(t, err, test.path, "the public wrapper defaults to safe")
			plan, err := PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicyAllow)
			require.NoError(t, err)
			result, err := ExecuteConversionPlan(context.Background(), nil, plan, test.request)
			require.NoError(t, err)
			assert.Contains(t, result.Diagnostics, types.ConversionDiagnostic{Code: "reasoning_controls_not_portable", Path: test.path, Message: requirements.Diagnostics[0].Message, Severity: types.ConversionDiagnosticError, From: requirements.Format, To: types.RelayFormatOpenAI})
		})
	}
}

func TestConfiguredSearchConversionIsNotLossless(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"web_search_options":{"search_context_size":"high"}}`), request))
	requirements, err := AnalyzeRequest(request)
	require.NoError(t, err)
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
		_, err := PlanConversion(requirements, types.RelayFormatClaude, policy)
		require.ErrorContains(t, err, "web_search_options")
	}
	plan, err := PlanConversion(requirements, types.RelayFormatClaude, types.ConversionLossPolicyAllow)
	require.NoError(t, err)
	result, err := ExecuteConversionPlan(context.Background(), nil, plan, request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Diagnostics, "explicit allow must report the context-size to max-uses approximation")
	_, err = PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
}

func TestOpenAIReasoningEffortPreservedOnlyByEquivalentProjection(t *testing.T) {
	for _, test := range []struct {
		name, body string
		request    any
		target     types.RelayFormat
	}{
		{"chat", `{"model":"test","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatOpenAIResponses},
		{"responses", `{"model":"test","max_output_tokens":100,"input":"hello","reasoning":{"effort":"high"}}`, &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			plan, err := PlanConversion(requirements, test.target, types.ConversionLossPolicyStrict)
			require.NoError(t, err)
			result, err := ExecuteConversionPlan(context.Background(), nil, plan, test.request)
			require.NoError(t, err)
			switch value := result.Value.(type) {
			case *dto.GeneralOpenAIRequest:
				assert.Equal(t, "high", value.ReasoningEffort)
			case *dto.OpenAIResponsesRequest:
				require.NotNil(t, value.Reasoning)
				assert.Equal(t, "high", value.Reasoning.Effort)
			default:
				t.Fatalf("unexpected converted request %T", result.Value)
			}
			for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
					_, err = PlanConversion(requirements, target, policy)
					require.ErrorContains(t, err, "effort")
				}
			}
		})
	}
}

func TestResponsesReasoningSummaryLossIsDiagnosed(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","input":"hello","reasoning":{"effort":"high","summary":"auto"}}`), request))
	requirements, err := AnalyzeRequest(request)
	require.NoError(t, err)
	_, err = PlanConversion(requirements, types.RelayFormatOpenAIResponses, types.ConversionLossPolicyStrict)
	require.NoError(t, err)
	_, err = PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicySafe)
	require.ErrorContains(t, err, "reasoning.summary")
	plan, err := PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicyAllow)
	require.NoError(t, err)
	result, err := ExecuteConversionPlan(context.Background(), nil, plan, request)
	require.NoError(t, err)
	require.NotEmpty(t, result.Diagnostics)
}
