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

func chatToolRequestForClaude(model string, toolChoice any, reasoningEffort string) *dto.GeneralOpenAIRequest {
	maxTokens := uint(4096)
	return &dto.GeneralOpenAIRequest{
		Model:     model,
		MaxTokens: &maxTokens,
		Messages:  []dto.Message{{Role: "user", Content: "hi"}},
		Tools: []dto.ToolCallRequest{{
			Type:     "function",
			Function: dto.FunctionRequest{Name: "lookup", Parameters: map[string]any{"type": "object"}},
		}},
		ToolChoice:      toolChoice,
		ReasoningEffort: reasoningEffort,
	}
}

func marshaledField(t *testing.T, value any) string {
	t.Helper()
	encoded, err := kitutil.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}

func findDiagnostic(diagnostics []types.ConversionDiagnostic, code string) *types.ConversionDiagnostic {
	for index := range diagnostics {
		if diagnostics[index].Code == code {
			return &diagnostics[index]
		}
	}
	return nil
}

// Claude returns a 400 for a forced tool_choice on models that removed forced
// tool use and whenever manual extended thinking is enabled. A converted
// request must reach Claude as auto with a loss diagnostic; every other forced
// choice is unchanged.
func TestConvertedForcedToolChoiceIsDowngradedWhereClaudeRejectsIt(t *testing.T) {
	t.Parallel()

	named := map[string]any{"type": "function", "function": map[string]any{"name": "lookup"}}
	tests := []struct {
		name           string
		request        any
		wantChoice     string
		wantDowngraded bool
	}{
		{name: "manual thinking required", request: chatToolRequestForClaude("claude-sonnet-4-5", "required", "high"), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "manual thinking named", request: chatToolRequestForClaude("claude-sonnet-4-5", named, "high"), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "opus 5.5 named", request: chatToolRequestForClaude("claude-opus-5-5", named, ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "sonnet 5.5 required", request: chatToolRequestForClaude("claude-sonnet-5-5", "required", ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "fable 5.1 required", request: chatToolRequestForClaude("claude-fable-5-1", "required", ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		// Bedrock and namespaced channel names resolve to the same model row.
		{name: "bedrock opus 5.5 required", request: chatToolRequestForClaude("us.anthropic.claude-opus-5-5-20260101-v1:0", "required", ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "namespaced sonnet 5.5 named", request: chatToolRequestForClaude("anthropic/claude-sonnet-5.5", named, ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		// Dotted versions keep their row whatever follows them.
		{name: "dotted opus 5.5 thinking variant required", request: chatToolRequestForClaude("anthropic/claude-opus-5.5:thinking", "required", ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "dotted sonnet 5.5 dated named", request: chatToolRequestForClaude("claude-sonnet-5.5-20260401", named, ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "dotted fable 5.1 nested namespace required", request: chatToolRequestForClaude("openrouter/anthropic/claude-fable-5.1", "required", ""), wantChoice: `{"type":"auto"}`, wantDowngraded: true},
		{name: "opus 5 adaptive keeps any", request: chatToolRequestForClaude("claude-opus-5", "required", "high"), wantChoice: `{"type":"any"}`},
		{name: "no thinking keeps tool", request: chatToolRequestForClaude("claude-sonnet-4-5", named, ""), wantChoice: `{"name":"lookup","type":"tool"}`},
	}
	responses := &dto.OpenAIResponsesRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"claude-opus-5-5","max_output_tokens":4096,"input":"hi","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}],"tool_choice":"required","parallel_tool_calls":false}`), responses))
	tests = append(tests, struct {
		name           string
		request        any
		wantChoice     string
		wantDowngraded bool
	}{name: "responses opus 5.5 keeps serial tools", request: responses, wantChoice: `{"disable_parallel_tool_use":true,"type":"auto"}`, wantDowngraded: true})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, tt.request)
			require.NoError(t, err)
			claudeRequest := result.Value.(*dto.ClaudeRequest)
			assert.JSONEq(t, tt.wantChoice, marshaledField(t, claudeRequest.ToolChoice))

			diagnostic := findDiagnostic(result.Diagnostics, "forced_tool_choice_downgraded")
			if !tt.wantDowngraded {
				assert.Nil(t, diagnostic)
				return
			}
			require.NotNil(t, diagnostic)
			assert.Equal(t, "tool_choice", diagnostic.Path)
			assert.Equal(t, types.ConversionDiagnosticWarning, diagnostic.Severity)
			assert.EqualValues(t, types.RelayFormatClaude, diagnostic.To)
		})
	}
}

func TestStrictPolicyRejectsForcedToolChoiceDowngrade(t *testing.T) {
	t.Parallel()

	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
		t.Run(string(policy), func(t *testing.T) {
			t.Parallel()

			meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}}
			_, err := ConvertRequest(context.Background(), meta, types.RelayFormatClaude, chatToolRequestForClaude("claude-opus-5-5", "required", ""))
			if policy == types.ConversionLossPolicySafe {
				require.NoError(t, err)
				return
			}
			var loss *types.ConversionLossError
			require.ErrorAs(t, err, &loss)
			assert.NotNil(t, findDiagnostic(loss.Diagnostics, "forced_tool_choice_downgraded"))
		})
	}
}

// A converted request that disables reasoning reaches Claude with a thinking
// mode the model accepts; between_tools carries no other thinking field.
func TestDisabledReasoningReachesClaudeAsAcceptedThinking(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model          string
		wantThinking   string
		wantOutput     string
		wantDiagnostic string
	}{
		{model: "claude-opus-5-5", wantThinking: `{"type":"adaptive"}`, wantOutput: `{"effort":"low"}`, wantDiagnostic: "claude_thinking_disable_unsupported"},
		{model: "claude-sonnet-5-5", wantThinking: `{"type":"between_tools"}`, wantDiagnostic: "claude_thinking_disable_as_between_tools"},
		{model: "claude-opus-5", wantThinking: `{"type":"disabled"}`},
		{model: "us.anthropic.claude-opus-5-5-20260101-v1:0", wantThinking: `{"type":"adaptive"}`, wantOutput: `{"effort":"low"}`, wantDiagnostic: "claude_thinking_disable_unsupported"},
		{model: "global.anthropic.claude-sonnet-5-5-v1:0", wantThinking: `{"type":"between_tools"}`, wantDiagnostic: "claude_thinking_disable_as_between_tools"},
		{model: "anthropic/claude-sonnet-5.5:thinking", wantThinking: `{"type":"between_tools"}`, wantDiagnostic: "claude_thinking_disable_as_between_tools"},
		{model: "claude-opus-5.5-20260401", wantThinking: `{"type":"adaptive"}`, wantOutput: `{"effort":"low"}`, wantDiagnostic: "claude_thinking_disable_unsupported"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			t.Parallel()

			maxTokens := uint(4096)
			request := &dto.GeneralOpenAIRequest{
				Model:           tt.model,
				MaxTokens:       &maxTokens,
				Messages:        []dto.Message{{Role: "user", Content: "hi"}},
				ReasoningEffort: "none",
			}
			result, err := ConvertRequest(context.Background(), nil, types.RelayFormatClaude, request)
			require.NoError(t, err)
			claudeRequest := result.Value.(*dto.ClaudeRequest)
			assert.JSONEq(t, tt.wantThinking, marshaledField(t, claudeRequest.Thinking))
			if tt.wantOutput == "" {
				assert.Empty(t, claudeRequest.OutputConfig)
			} else {
				assert.JSONEq(t, tt.wantOutput, string(claudeRequest.OutputConfig))
			}
			if tt.wantDiagnostic == "" {
				assert.Nil(t, findDiagnostic(result.Diagnostics, "claude_thinking_disable_unsupported"))
				assert.Nil(t, findDiagnostic(result.Diagnostics, "claude_thinking_disable_as_between_tools"))
				return
			}
			diagnostic := findDiagnostic(result.Diagnostics, tt.wantDiagnostic)
			require.NotNil(t, diagnostic)
			assert.Equal(t, types.ConversionDiagnosticWarning, diagnostic.Severity)
		})
	}
}
