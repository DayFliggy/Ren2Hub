package relayconvert

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A cross-protocol request carries reasoning strength only in the model name
// when the caller supplies no conversion state. Anthropic rejects a suffixed
// name, so the tail must be resolved during conversion rather than forwarded.
func TestCrossProtocolConversionResolvesClaudeModelSuffix(t *testing.T) {
	for _, test := range []struct {
		name   string
		model  string
		effort string
	}{
		{name: "effort tail", model: "claude-opus-4-8-high", effort: "high"},
		{name: "thinking alias", model: "claude-opus-4-8-thinking", effort: "high"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{
				Model:       test.model,
				Messages:    []dto.Message{{Role: "user", Content: "hello"}},
				MaxTokens:   kitutil.GetPointer(uint(4096)),
				Temperature: kitutil.GetPointer(0.7),
			}
			info := &convmeta.Values{Options: &convmeta.Options{
				Claude: convmeta.ClaudeOptions{ThinkingAdapterEnabled: true},
			}}

			result, err := ConvertRequest(nil, info, types.RelayFormatClaude, request)
			require.NoError(t, err)
			converted, ok := result.Value.(*dto.ClaudeRequest)
			require.True(t, ok)

			assert.Equal(t, "claude-opus-4-8", converted.Model)
			require.NotNil(t, converted.Thinking)
			assert.Equal(t, "adaptive", converted.Thinking.Type)
			assert.JSONEq(t, `{"effort":"`+test.effort+`"}`, string(converted.OutputConfig))
		})
	}
}

// PreserveThinkingSuffix keeps a cascaded downstream gateway able to recover
// intent from the model name, so the tail must survive conversion untouched.
func TestPreserveThinkingSuffixKeepsClaudeModelName(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{
		Model:     "claude-opus-4-8-thinking",
		Messages:  []dto.Message{{Role: "user", Content: "hello"}},
		MaxTokens: kitutil.GetPointer(uint(4096)),
	}
	info := &convmeta.Values{Options: &convmeta.Options{
		Claude:                 convmeta.ClaudeOptions{ThinkingAdapterEnabled: true},
		PreserveThinkingSuffix: func(string) bool { return true },
	}}

	result, err := ConvertRequest(nil, info, types.RelayFormatClaude, request)
	require.NoError(t, err)
	converted, ok := result.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.Equal(t, "claude-opus-4-8-thinking", converted.Model)
}

// A native Claude request is already in the target protocol. Its name is
// forwarded verbatim because a Claude-compatible proxy may define its own
// suffix vocabulary that this converter must not reinterpret.
func TestNativeClaudeRequestKeepsModelNameVerbatim(t *testing.T) {
	request := &dto.ClaudeRequest{
		Model:     "claude-opus-4-8-high",
		MaxTokens: kitutil.GetPointer(uint(4096)),
		Messages:  []dto.ClaudeMessage{{Role: "user", Content: "hello"}},
	}
	info := &convmeta.Values{Options: &convmeta.Options{
		Claude: convmeta.ClaudeOptions{ThinkingAdapterEnabled: true},
	}}

	result, err := ConvertRequest(nil, info, types.RelayFormatClaude, request)
	require.NoError(t, err)
	converted, ok := result.Value.(*dto.ClaudeRequest)
	require.True(t, ok)
	assert.Equal(t, "claude-opus-4-8-high", converted.Model)
}
