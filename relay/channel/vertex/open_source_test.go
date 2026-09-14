package vertex

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenSourceOutputLimitPreservesPresence(t *testing.T) {
	zero, eight := uint(0), uint(8)
	for _, scenario := range []struct {
		name                      string
		max, completion, expected *uint
	}{
		{"missing", nil, nil, nil},
		{"zero", &zero, nil, &zero},
		{"completion", nil, &eight, &eight},
		{"completion_zero_precedence", &eight, &zero, &zero},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{MaxTokens: scenario.max, MaxCompletionTokens: scenario.completion}
			adaptor := &Adaptor{RequestMode: RequestModeOpenSource}
			value, err := adaptor.ConvertOpenAIRequest(nil, nil, request)
			require.NoError(t, err)
			converted, ok := value.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)
			assert.Equal(t, scenario.expected, converted.MaxTokens)
			assert.Nil(t, converted.MaxCompletionTokens)
			assert.Equal(t, scenario.max, request.MaxTokens)
			assert.Equal(t, scenario.completion, request.MaxCompletionTokens)
		})
	}
}
