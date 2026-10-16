package reasoning

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestGeminiArgonDocumentedLevelsAndDefault(t *testing.T) {
	for _, tc := range []struct {
		effort     Effort
		want       string
		diagnostic bool
	}{
		{EffortLow, "low", false}, {EffortMedium, "medium", false}, {EffortHigh, "high", false}, {EffortMinimal, "low", true}, {EffortXHigh, "high", true}, {EffortNone, "low", true},
	} {
		t.Run(string(tc.effort), func(t *testing.T) {
			render, err := RenderGemini("gemini-4-argon", Intent{Effort: tc.effort}, nil, 0)
			require.NoError(t, err)
			require.NotNil(t, render.Config)
			assert.Equal(t, tc.want, render.Config.ThinkingLevel)
			assert.Nil(t, render.Config.ThinkingBudget)
			assert.Equal(t, tc.diagnostic, len(render.Diagnostics) > 0)
			native := &dto.GeminiThinkingConfig{ThinkingLevel: string(tc.effort)}
			effective, diagnostics, err := NormalizeGeminiThinkingConfig("gemini-4-argon", &dto.GeminiChatGenerationConfig{ThinkingConfig: native})
			require.NoError(t, err)
			assert.Equal(t, tc.want, native.ThinkingLevel)
			assert.Equal(t, Effort(tc.want), effective)
			assert.Equal(t, tc.diagnostic, len(diagnostics) > 0)
		})
	}
	assert.Equal(t, EffortMedium, ResolveGeminiDefault("gemini-4-argon", Intent{}).Effort)
	for _, model := range []string{"gemini-4-other", "gemini-4-argon-future", "gemini-5-pro"} {
		render, err := RenderGemini(model, Intent{Effort: EffortMedium}, nil, 0)
		require.NoError(t, err)
		assert.Nil(t, render.Config)
		require.Len(t, render.Diagnostics, 1)
		assert.Equal(t, "gemini_unknown_capability", render.Diagnostics[0].Code)
	}
}
