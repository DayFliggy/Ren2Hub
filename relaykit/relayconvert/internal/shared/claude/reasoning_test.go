package claude

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyReasoningPreservesOnlyNativeThinkingControls(t *testing.T) {
	const originalThinking = `{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_enabled":false,"future_limit":0,"future_policy":null}`
	for _, tt := range []struct {
		name, model, wantType string
		mode                  reasoning.Mode
		crossProtocol         bool
	}{
		{"native adaptive", "claude-opus-5-5", "adaptive", reasoning.ModeEnabled, false},
		{"native between tools", "claude-sonnet-5-5", "between_tools", reasoning.ModeDisabled, false},
		{"native disabled", "claude-opus-4-6", "disabled", reasoning.ModeDisabled, false},
		{"native manual", "claude-sonnet-4-5", "enabled", reasoning.ModeEnabled, false},
		{"converted adaptive", "claude-opus-5-5", "adaptive", reasoning.ModeEnabled, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var thinking dto.Thinking
			require.NoError(t, kitutil.Unmarshal([]byte(originalThinking), &thinking))
			request := &dto.ClaudeRequest{Model: tt.model, MaxTokens: kitutil.GetPointer(uint(4096)), Thinking: &thinking}
			meta := &convmeta.Values{ReasoningConversion: reasoning.StateFromIntent(reasoning.Intent{Mode: tt.mode, Effort: reasoning.EffortHigh})}
			require.NoError(t, ApplyReasoning(context.Background(), request, meta, reasoning.Intent{}, tt.crossProtocol))
			require.NotNil(t, request.Thinking)
			assert.Equal(t, tt.wantType, request.Thinking.Type)
			if tt.crossProtocol {
				assert.Empty(t, request.Thinking.Extra)
			} else {
				assert.Equal(t, thinking.Extra, request.Thinking.Extra)
			}
			if !tt.crossProtocol && tt.wantType == "adaptive" {
				assert.Equal(t, thinking.BlockBinding, request.Thinking.BlockBinding)
			} else {
				assert.Empty(t, request.Thinking.BlockBinding)
			}
			encoded, err := kitutil.Marshal(thinking)
			require.NoError(t, err)
			assert.JSONEq(t, originalThinking, string(encoded), "rendering must not change the prior thinking value")
		})
	}
}
