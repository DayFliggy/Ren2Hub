package reasoning

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Known Claude models get the nearest effort level they accept, and none when
// they accept none; each change is reported (NewAPI 036ba376d). Every
// combination listed here returns a 400 from the Messages API when sent as
// requested.
func TestClaudeEffortIsSentOnlyAtALevelTheModelAccepts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		model         string
		intent        Intent
		wantThinking  string
		wantOutput    Effort
		wantEffective Effort
		wantCodes     []string
	}{
		{name: "opus 4.5 caps effort-only max at high", model: "claude-opus-4-5", intent: Intent{Effort: EffortMax},
			wantOutput: EffortHigh, wantEffective: EffortHigh, wantCodes: []string{"claude_effort_adjusted"}},
		{name: "sonnet 4.5 takes no effort", model: "claude-sonnet-4-5", intent: Intent{Effort: EffortHigh},
			wantEffective: EffortNone, wantCodes: []string{"claude_effort_unsupported"}},
		{name: "haiku 4.5 takes no effort with manual thinking", model: "claude-haiku-4-5", intent: Intent{Mode: ModeEnabled, Effort: EffortMedium},
			wantThinking: "enabled", wantEffective: EffortMedium},
		{name: "opus 5 disabled thinking caps max at high", model: "claude-opus-5", intent: Intent{Mode: ModeDisabled, Effort: EffortMax},
			wantThinking: "disabled", wantOutput: EffortHigh, wantEffective: EffortNone, wantCodes: []string{"claude_effort_adjusted"}},
		{name: "opus 5 disabled thinking keeps medium", model: "claude-opus-5", intent: Intent{Mode: ModeDisabled, Effort: EffortMedium},
			wantThinking: "disabled", wantOutput: EffortMedium, wantEffective: EffortNone},
		{name: "haiku 5.5 disabled thinking caps xhigh at high", model: "claude-haiku-5.5", intent: Intent{Mode: ModeDisabled, Effort: EffortXHigh},
			wantThinking: "disabled", wantOutput: EffortHigh, wantEffective: EffortNone, wantCodes: []string{"claude_effort_adjusted"}},
		{name: "sonnet 5.5 between_tools caps xhigh at high", model: "claude-sonnet-5-5", intent: Intent{Mode: ModeDisabled, Effort: EffortXHigh},
			wantThinking: "between_tools", wantOutput: EffortHigh, wantEffective: EffortNone,
			wantCodes: []string{"claude_effort_adjusted", "claude_thinking_disable_as_between_tools"}},
		{name: "sonnet 4.5 disabled thinking drops effort", model: "claude-sonnet-4-5", intent: Intent{Mode: ModeDisabled, Effort: EffortHigh},
			wantThinking: "disabled", wantEffective: EffortNone, wantCodes: []string{"claude_effort_unsupported"}},
		{name: "fable 5 cannot disable thinking and runs at low", model: "claude-fable-5", intent: Intent{Mode: ModeDisabled, Effort: EffortNone},
			wantThinking: "adaptive", wantOutput: EffortLow, wantEffective: EffortLow, wantCodes: []string{"claude_thinking_disable_unsupported"}},
	}
	maxTokens := uint(8192)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rendered, err := RenderClaude(tt.model, tt.intent, &maxTokens, 0)
			require.NoError(t, err)
			if tt.wantThinking == "" {
				assert.Nil(t, rendered.Thinking)
			} else {
				require.NotNil(t, rendered.Thinking)
				assert.Equal(t, tt.wantThinking, rendered.Thinking.Type)
			}
			assert.Equal(t, tt.wantOutput, rendered.OutputEffort)
			assert.Equal(t, tt.wantEffective, rendered.EffectiveEffort)
			assert.Equal(t, tt.wantCodes, diagnosticCodes(rendered.Diagnostics))
		})
	}
}

// Claude Haiku 5.5 takes adaptive thinking only, defaults to medium effort,
// and accepts sampling controls only at their defaults (AUDIT1009B-09).
func TestClaudeHaiku55RendersAdaptiveThinkingWithoutSampling(t *testing.T) {
	t.Parallel()

	budget := 8000
	maxTokens := uint(16000)
	for _, model := range []string{"claude-haiku-5-5", "claude-haiku-5.5", "us.anthropic.claude-haiku-5-5-20261001-v1:0"} {
		t.Run(model, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, EffortMedium, ClaudeDefaultEffort(model))
			rendered, err := RenderClaude(model, Intent{Mode: ModeEnabled, BudgetTokens: &budget}, &maxTokens, 0)
			require.NoError(t, err)
			require.NotNil(t, rendered.Thinking)
			assert.Equal(t, "adaptive", rendered.Thinking.Type)
			assert.Nil(t, rendered.Thinking.BudgetTokens)
			assert.True(t, rendered.ClearSampling)
			assert.Contains(t, diagnosticCodes(rendered.Diagnostics), "claude_budget_to_adaptive")
		})
	}
}

// Gemini 2.5 Pro takes budgets up to 32768, so xhigh and max get its largest
// budget; high keeps Google's 24576 (NewAPI 036ba376d).
func TestGemini25EffortBudgetsFollowTheModelMaximum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		model  string
		effort Effort
		want   int
	}{
		{model: "gemini-2.5-pro", effort: EffortHigh, want: 24576},
		{model: "gemini-2.5-pro", effort: EffortXHigh, want: 32768},
		{model: "gemini-2.5-pro", effort: EffortMax, want: 32768},
		{model: "gemini-2.5-flash", effort: EffortMax, want: 24576},
	}
	for _, tt := range tests {
		t.Run(tt.model+"/"+string(tt.effort), func(t *testing.T) {
			t.Parallel()

			rendered, err := RenderGemini(tt.model, Intent{Mode: ModeEnabled, Effort: tt.effort}, nil, 0)
			require.NoError(t, err)
			require.NotNil(t, rendered.Config)
			require.NotNil(t, rendered.Config.ThinkingBudget)
			assert.Equal(t, tt.want, *rendered.Config.ThinkingBudget)
		})
	}
}
