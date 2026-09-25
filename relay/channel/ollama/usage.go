package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// A successful terminal confirms output, not absent token counters. Keep
// missing and null values unconfirmed so accepted requests retain their funds.
func ollamaTokenUsage(counters ...json.RawMessage) (*dto.Usage, error) {
	if len(counters) < 1 || len(counters) > 2 {
		return nil, fmt.Errorf("invalid ollama usage dimensions")
	}
	var counts [2]int
	present, invalid := 0, false
	for index, raw := range counters {
		if len(raw) == 0 {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			invalid = true
			continue
		}
		if err := common.Unmarshal(raw, &counts[index]); err != nil || counts[index] < 0 {
			return nil, fmt.Errorf("invalid ollama token usage")
		}
		present++
	}
	if counts[0] > math.MaxInt-counts[1] {
		return nil, fmt.Errorf("invalid ollama token usage")
	}
	source := "provider"
	switch {
	case invalid:
		source = "invalid"
	case present == 0:
		source = "missing"
	case present != len(counters):
		source = "partial"
	}
	return &dto.Usage{PromptTokens: counts[0], CompletionTokens: counts[1], TotalTokens: counts[0] + counts[1], UsageSource: source}, nil
}
