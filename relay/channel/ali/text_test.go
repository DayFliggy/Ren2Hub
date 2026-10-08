package ali

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestOpenAI2AliTopP(t *testing.T) {
	tests := []struct {
		name string
		topP *float64
		want *float64
	}{
		{
			name: "omitted top_p is not injected",
			topP: nil,
			want: nil,
		},
		{
			name: "in-range top_p is preserved",
			topP: lo.ToPtr(0.8),
			want: lo.ToPtr(0.8),
		},
		{
			name: "top_p of 1 is clamped to two decimals",
			topP: lo.ToPtr(1.0),
			want: lo.ToPtr(0.99),
		},
		{
			name: "top_p above 1 is clamped to two decimals",
			topP: lo.ToPtr(1.5),
			want: lo.ToPtr(0.99),
		},
		{
			name: "top_p of 0 is clamped to two decimals",
			topP: lo.ToPtr(0.0),
			want: lo.ToPtr(0.01),
		},
		{
			name: "negative top_p is clamped to two decimals",
			topP: lo.ToPtr(-0.3),
			want: lo.ToPtr(0.01),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requestOpenAI2Ali(dto.GeneralOpenAIRequest{
				Model: "qwen-plus",
				TopP:  tt.topP,
			}, "qwen-plus", "")
			require.NoError(t, err)

			assert.Equal(t, tt.want, got.TopP)
		})
	}
}

// TestRequestOpenAI2AliDisabledThinking covers the DashScope-native rendering of
// a disabling reasoning intent: Bailian rejects reasoning_effort "none" instead
// of turning thinking off, so the adaptor must send enable_thinking false and
// leave the unsupported field out.
func TestRequestOpenAI2AliDisabledThinking(t *testing.T) {
	tests := []struct {
		name           string
		enableThinking json.RawMessage
		resolvedEffort string
		wantThinking   any
		wantEffort     string
		wantErr        bool
	}{
		{
			name:           "disabled intent renders the native switch",
			resolvedEffort: string(reasoning.EffortNone),
			wantThinking:   false,
		},
		{
			name:           "client enable_thinking false is preserved",
			enableThinking: json.RawMessage("false"),
			resolvedEffort: string(reasoning.EffortNone),
			wantThinking:   false,
		},
		{
			name:           "contradictory enable_thinking true is rejected",
			enableThinking: json.RawMessage("true"),
			resolvedEffort: string(reasoning.EffortNone),
			wantErr:        true,
		},
		{
			name:           "enabled intent keeps enable_thinking and effort",
			enableThinking: json.RawMessage("true"),
			resolvedEffort: string(reasoning.EffortHigh),
			wantThinking:   true,
			wantEffort:     "high",
		},
		{
			name:           "unset intent is untouched",
			enableThinking: json.RawMessage("true"),
			wantThinking:   true,
			wantEffort:     "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := requestOpenAI2Ali(dto.GeneralOpenAIRequest{
				Model:           "qwen-plus",
				ReasoningEffort: "high",
				EnableThinking:  tt.enableThinking,
			}, "qwen-plus", tt.resolvedEffort)

			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, reasoning.IsClientError(err))
				assert.Nil(t, got)
				return
			}
			require.NoError(t, err)

			if tt.wantThinking == nil {
				assert.Empty(t, got.EnableThinking)
			} else {
				assert.Equal(t, tt.wantThinking, gjsonBool(t, got.EnableThinking))
			}
			// A disabling intent replaces the unsupported reasoning_effort with
			// the native switch; every other intent passes the field through.
			assert.Equal(t, tt.wantEffort, got.ReasoningEffort)
		})
	}
}

func gjsonBool(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var value any
	require.NoError(t, json.Unmarshal(raw, &value))
	return value
}
