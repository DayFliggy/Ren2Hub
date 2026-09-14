package service

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/relaykit/dto"
)

// NormalizeRealtimeUsage validates provider counts before they can multiply a
// price. Missing text detail is the remainder of the declared directional total.
func NormalizeRealtimeUsage(usage *dto.RealtimeUsage) (*dto.RealtimeUsage, error) {
	if usage == nil {
		return nil, errors.New("realtime usage is missing")
	}
	result := *usage
	values := []int{result.TotalTokens, result.InputTokens, result.OutputTokens,
		result.InputTokenDetails.TextTokens, result.InputTokenDetails.AudioTokens,
		result.InputTokenDetails.CachedTokens, result.OutputTokenDetails.TextTokens,
		result.InputTokenDetails.ImageTokens, result.InputTokenDetails.CachedCreationTokens, result.InputTokenDetails.CacheWriteTokens,
		result.OutputTokenDetails.AudioTokens, result.OutputTokenDetails.ImageTokens, result.OutputTokenDetails.ReasoningTokens}
	for _, value := range values {
		if value < 0 || value > math.MaxInt32 {
			return nil, errors.New("realtime usage token count is out of range")
		}
	}
	inputDetails := int64(result.InputTokenDetails.TextTokens) + int64(result.InputTokenDetails.AudioTokens)
	outputDetails := int64(result.OutputTokenDetails.TextTokens) + int64(result.OutputTokenDetails.AudioTokens)
	if result.InputTokens > 0 && inputDetails > int64(result.InputTokens) ||
		result.OutputTokens > 0 && outputDetails > int64(result.OutputTokens) {
		return nil, errors.New("realtime usage details exceed directional totals")
	}
	input := max(int64(result.InputTokens), inputDetails)
	output := max(int64(result.OutputTokens), outputDetails)
	if input+output > math.MaxInt32 || result.TotalTokens > 0 && int64(result.TotalTokens) != input+output ||
		int64(result.InputTokenDetails.CachedTokens) > input || int64(result.InputTokenDetails.ImageTokens) > input ||
		int64(result.InputTokenDetails.CachedCreationTokens) > input || int64(result.InputTokenDetails.CacheWriteTokens) > input ||
		int64(result.OutputTokenDetails.ImageTokens) > output || int64(result.OutputTokenDetails.ReasoningTokens) > output {
		return nil, errors.New("realtime usage totals are invalid")
	}
	result.InputTokenDetails.TextTokens += int(input - inputDetails)
	result.OutputTokenDetails.TextTokens += int(output - outputDetails)
	result.InputTokens = int(input)
	result.OutputTokens = int(output)
	result.TotalTokens = int(input + output)
	return &result, nil
}

// AddRealtimeUsage does not mutate either input if validation fails.
func AddRealtimeUsage(total, next *dto.RealtimeUsage) (*dto.RealtimeUsage, error) {
	left, err := NormalizeRealtimeUsage(total)
	if err != nil {
		return nil, err
	}
	right, err := NormalizeRealtimeUsage(next)
	if err != nil {
		return nil, err
	}
	if int64(left.TotalTokens)+int64(right.TotalTokens) > math.MaxInt32 {
		return nil, errors.New("realtime session usage exceeds the token limit")
	}
	left.TotalTokens += right.TotalTokens
	left.InputTokens += right.InputTokens
	left.OutputTokens += right.OutputTokens
	left.InputTokenDetails.TextTokens += right.InputTokenDetails.TextTokens
	left.InputTokenDetails.AudioTokens += right.InputTokenDetails.AudioTokens
	left.InputTokenDetails.CachedTokens += right.InputTokenDetails.CachedTokens
	left.OutputTokenDetails.TextTokens += right.OutputTokenDetails.TextTokens
	left.OutputTokenDetails.AudioTokens += right.OutputTokenDetails.AudioTokens
	return left, nil
}
