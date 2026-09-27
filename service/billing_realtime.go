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
	result.InputTokenDetails = usage.InputTokenDetails.Clone()
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
	inputDetails := int64(result.InputTokenDetails.TextTokens) + int64(result.InputTokenDetails.AudioTokens) + int64(result.InputTokenDetails.ImageTokens)
	outputDetails := int64(result.OutputTokenDetails.TextTokens) + int64(result.OutputTokenDetails.AudioTokens) + int64(result.OutputTokenDetails.ImageTokens)
	// A complete provider observation preserves explicit zero totals. Local
	// estimates may omit totals and derive them from their partial details.
	confirmed := result.Observation != nil && result.Observation.Complete && !result.Observation.Estimated
	if (confirmed || result.InputTokens > 0) && inputDetails > int64(result.InputTokens) ||
		(confirmed || result.OutputTokens > 0) && outputDetails > int64(result.OutputTokens) {
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
	if cached := result.InputTokenDetails.CachedTokensDetails; cached != nil {
		sum := int64(0)
		for _, count := range []*int{cached.TextTokens, cached.AudioTokens, cached.ImageTokens} {
			if count != nil {
				if *count < 0 || *count > math.MaxInt32 {
					return nil, errors.New("realtime cache detail is out of range")
				}
				sum += int64(*count)
			}
		}
		if sum > int64(result.InputTokenDetails.CachedTokens) ||
			cached.AudioTokens != nil && *cached.AudioTokens > result.InputTokenDetails.AudioTokens ||
			cached.ImageTokens != nil && *cached.ImageTokens > result.InputTokenDetails.ImageTokens {
			return nil, errors.New("realtime cache detail exceeds its parent count")
		}
	}
	// These are alternate names for the same write bucket. Normalize before
	// addition, so different dialects in two responses cannot undercount it.
	result.InputTokenDetails.CachedCreationTokens = result.InputTokenDetails.CacheCreationTokensTotal()
	result.InputTokenDetails.CacheWriteTokens = 0
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
	left.InputTokenDetails.CachedTokensDetails = addRealtimeCachedDetails(left.InputTokenDetails, right.InputTokenDetails)
	left.InputTokenDetails.TextTokens += right.InputTokenDetails.TextTokens
	left.InputTokenDetails.AudioTokens += right.InputTokenDetails.AudioTokens
	left.InputTokenDetails.ImageTokens += right.InputTokenDetails.ImageTokens
	left.InputTokenDetails.CachedCreationTokens += right.InputTokenDetails.CachedCreationTokens
	left.InputTokenDetails.CachedTokens += right.InputTokenDetails.CachedTokens
	left.OutputTokenDetails.TextTokens += right.OutputTokenDetails.TextTokens
	left.OutputTokenDetails.AudioTokens += right.OutputTokenDetails.AudioTokens
	left.OutputTokenDetails.ImageTokens += right.OutputTokenDetails.ImageTokens
	left.OutputTokenDetails.ReasoningTokens += right.OutputTokenDetails.ReasoningTokens
	return left, nil
}

func addRealtimeCachedDetails(left, right dto.InputTokenDetails) *dto.CachedTokenDetails {
	if left.CachedTokensDetails == nil && right.CachedTokensDetails == nil {
		return nil
	}
	l, r := left.CachedTokensDetails, right.CachedTokensDetails
	if l == nil {
		l = &dto.CachedTokenDetails{}
	}
	if r == nil {
		r = &dto.CachedTokenDetails{}
	}
	add := func(a, b *int) *int {
		// A missing subtype on a response with cached tokens makes the session
		// subtype unknown; reporting only the other responses would undercharge.
		if a == nil && left.CachedTokens > 0 || b == nil && right.CachedTokens > 0 {
			return nil
		}
		if a == nil && b == nil {
			return nil
		}
		sum := 0
		if a != nil {
			sum += *a
		}
		if b != nil {
			sum += *b
		}
		return &sum
	}
	return &dto.CachedTokenDetails{TextTokens: add(l.TextTokens, r.TextTokens),
		AudioTokens: add(l.AudioTokens, r.AudioTokens), ImageTokens: add(l.ImageTokens, r.ImageTokens)}
}
