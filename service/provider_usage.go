package service

import (
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// NormalizeProviderUsage rejects inconsistent cumulative token counters before
// they become trusted billing or stream completion evidence.
func NormalizeProviderUsage(usage *dto.Usage) error {
	if usage == nil {
		return nil
	}
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 || usage.PromptTokens > math.MaxInt-usage.CompletionTokens {
		return fmt.Errorf("provider returned invalid token usage")
	}
	total := usage.PromptTokens + usage.CompletionTokens
	if usage.TotalTokens == 0 {
		usage.TotalTokens = total
	} else if usage.TotalTokens < total {
		return fmt.Errorf("provider total usage is smaller than input and output usage")
	}
	return nil
}
