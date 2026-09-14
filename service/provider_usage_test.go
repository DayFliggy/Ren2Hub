package service

import (
	"math"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeProviderUsage(t *testing.T) {
	for _, test := range []struct {
		name    string
		usage   dto.Usage
		total   int
		invalid bool
	}{
		{"missing_total", dto.Usage{PromptTokens: 10, CompletionTokens: 2}, 12, false},
		{"explicit_total", dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12}, 12, false},
		{"negative", dto.Usage{PromptTokens: -10, CompletionTokens: 2, TotalTokens: 12}, 0, true},
		{"overflow", dto.Usage{PromptTokens: math.MaxInt, CompletionTokens: 2}, 0, true},
		{"inconsistent", dto.Usage{PromptTokens: 10, CompletionTokens: 2, TotalTokens: 1}, 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := test.usage
			err := NormalizeProviderUsage(&test.usage)
			if test.invalid {
				require.Error(t, err)
				assert.Equal(t, original, test.usage)
			} else {
				require.NoError(t, err)
				assert.Equal(t, test.total, test.usage.TotalTokens)
			}
		})
	}
}
