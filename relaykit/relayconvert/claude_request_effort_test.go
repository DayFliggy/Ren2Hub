package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When a model-name suffix turns thinking off, the client's own
// output_config.effort is checked against the model instead of being sent
// unchanged (NewAPI 036ba376d): Opus 5 rejects disabled thinking at max, and
// Sonnet 4.5 takes no effort at all. Other output_config members survive.
func TestThinkingOffSuffixKeepsClientEffortWithinModelLimits(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		effort     string
		wantOutput string
	}{
		{name: "opus 5 max becomes high", model: "claude-opus-5", effort: "max", wantOutput: `{"effort":"high","format":{"type":"json_schema"}}`},
		{name: "opus 5 low stays", model: "claude-opus-5", effort: "low", wantOutput: `{"effort":"low","format":{"type":"json_schema"}}`},
		{name: "sonnet 4.5 effort is removed", model: "claude-sonnet-4-5", effort: "high", wantOutput: `{"format":{"type":"json_schema"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens := uint(4096)
			req := &dto.ClaudeRequest{
				Model:        tt.model,
				MaxTokens:    &maxTokens,
				Messages:     []dto.ClaudeMessage{{Role: "user", Content: "hi"}},
				OutputConfig: mustRawMessage(t, map[string]any{"effort": tt.effort, "format": map[string]any{"type": "json_schema"}}),
			}
			info := &convmeta.Values{ReasoningConversion: &dto.ReasoningConversionState{Mode: "disabled"}}

			require.NoError(t, ApplyClaudeThinkingModel(req, info))

			require.NotNil(t, req.Thinking)
			assert.Equal(t, "disabled", req.Thinking.Type)
			assert.JSONEq(t, tt.wantOutput, string(req.OutputConfig))
		})
	}
}
