package service

import (
	"math"
	"strings"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// NormalizedBillingUsage is the single protocol boundary used by unit prices
// and expressions. Counts are not inferred from a provider's monetary cost.
func NormalizedBillingUsage(usage *dto.Usage, semantic string) billing.Usage {
	var measured map[string]string
	var observation *dto.UsageObservation
	if usage != nil {
		observation = usage.Observation
		measured = usage.MeasuredQuantities
	}
	estimated := usage != nil && strings.Contains(usage.UsageSource, "estimated")
	unconfirmed := usage == nil || usage.UsageSource == "missing" || usage.UsageSource == "partial" || usage.UsageSource == "invalid"
	usage = effectiveBillingUsage(usage)
	if usage == nil {
		return billing.Usage{Estimated: true, Source: "missing", Quantities: map[string]string{}}
	}
	if usage.UsageSemantic != "" {
		semantic = usage.UsageSemantic
	}
	// Reject bad source quantities before display helpers can clamp them or
	// overlapping cache aliases can mask them during normalization.
	invalid := false
	for _, count := range []int{usage.PromptTokens, usage.CompletionTokens,
		usage.PromptTokensDetails.CachedTokens, usage.PromptTokensDetails.CachedCreationTokens,
		usage.PromptTokensDetails.CacheWriteTokens, usage.ClaudeCacheCreation5mTokens, usage.ClaudeCacheCreation1hTokens,
		usage.PromptTokensDetails.ImageTokens, usage.PromptTokensDetails.AudioTokens,
		usage.CompletionTokenDetails.ImageTokens, usage.CompletionTokenDetails.AudioTokens,
		usage.CompletionTokenDetails.ReasoningTokens} {
		invalid = invalid || count < 0 || int64(count) > math.MaxInt32
	}
	write := usage.PromptTokensDetails.CacheCreationTokensTotal()
	write1h := usage.ClaudeCacheCreation1hTokens
	if usage.ClaudeCacheCreation5mTokens > 0 || write1h > 0 {
		write = usage.ClaudeCacheCreation5mTokens
		if total := usage.PromptTokensDetails.CacheCreationTokensTotal() - write1h; total > write {
			write = total
		}
	}
	input, output := int64(usage.PromptTokens), int64(usage.CompletionTokens)
	context := input
	includesCache := semantic != "anthropic"
	if !includesCache {
		context += int64(usage.PromptTokensDetails.CachedTokens) + int64(write) + int64(write1h)
	}
	estimated = estimated || strings.Contains(usage.UsageSource, "estimated")
	if usage.BillingUsage != nil {
		estimated = estimated || usage.BillingUsage.Estimated
	}
	if observation != nil {
		estimated = estimated || observation.Estimated
		unconfirmed = unconfirmed || !observation.Complete
	}
	var audioCacheRead *int64
	if details := usage.PromptTokensDetails.CachedTokensDetails; details != nil && details.AudioTokens != nil {
		value := int64(*details.AudioTokens)
		audioCacheRead = &value
		invalid = invalid || value < 0 || value > math.MaxInt32
	}
	var imageCacheRead *int64
	if details := usage.PromptTokensDetails.CachedTokensDetails; details != nil && details.ImageTokens != nil {
		value := int64(*details.ImageTokens)
		imageCacheRead = &value
		invalid = invalid || value < 0 || value > math.MaxInt32
	}
	result := billing.Usage{InputTokens: input, OutputTokens: output, ContextTokens: context,
		CacheReadTokens: int64(usage.PromptTokensDetails.CachedTokens), ImageCacheReadTokens: imageCacheRead, AudioCacheReadTokens: audioCacheRead, CacheWriteTokens: int64(write), CacheWrite1hTokens: int64(write1h),
		ImageInputTokens: int64(usage.PromptTokensDetails.ImageTokens), ImageOutputTokens: int64(usage.CompletionTokenDetails.ImageTokens),
		AudioInputTokens: int64(usage.PromptTokensDetails.AudioTokens), AudioOutputTokens: int64(usage.CompletionTokenDetails.AudioTokens),
		ReasoningTokens:    int64(usage.CompletionTokenDetails.ReasoningTokens),
		InputIncludesCache: includesCache, Source: usage.UsageSource, Estimated: estimated, Complete: !unconfirmed && !estimated, Invalid: invalid, Quantities: map[string]string{}}
	if observation != nil {
		result.Source = observation.Source
		result.Terminal = observation.Terminal
		result.ObservationIDs = append([]string(nil), observation.ResponseIDs...)
	}
	for key, value := range measured {
		result.Quantities[key] = value
	}
	return result
}

// An input-only price cannot discard output evidence carried by an alias or a
// detail field just because the provider omitted its aggregate output count.
func embeddingHasOutputUsage(usage *dto.Usage) bool {
	sources := []*dto.Usage{usage, effectiveBillingUsage(usage)}
	if usage != nil && usage.BillingUsage != nil {
		sources = append(sources, usage.BillingUsage.OpenAIUsage)
	}
	for _, source := range sources {
		if source == nil {
			continue
		}
		if source.CompletionTokens != 0 || source.OutputTokens != 0 {
			return true
		}
		for _, details := range []*dto.OutputTokenDetails{&source.CompletionTokenDetails, source.OutputTokensDetails} {
			if details != nil && (details.TextTokens != 0 || details.ReasoningTokens != 0 || details.AudioTokens != 0 || details.ImageTokens != 0) {
				return true
			}
		}
	}
	return false
}

const (
	usageBillingPathLocal              = "local"
	usageBillingPathUpstream           = "upstream"
	usageBillingPathMixed              = "upstream_and_local"
	usageBillingPathOpenAI             = "billing-usage-openai"
	usageBillingPathOpenAIEstimated    = "billing-usage-openai-estimated"
	usageBillingPathAnthropic          = "billing-usage-anthropic"
	usageBillingPathAnthropicEstimated = "billing-usage-anthropic-estimated"
	usageBillingPathGemini             = "billing-usage-gemini"
	usageBillingPathGeminiEstimated    = "billing-usage-gemini-estimated"
)

func effectiveBillingUsage(usage *dto.Usage) *dto.Usage {
	if billingUsage, ok := usageFromBillingUsage(usage); ok {
		return billingUsage
	}
	return usage
}

func usageBillingPathForLog(isLocalCountTokens bool, usage *dto.Usage) string {
	if usage != nil {
		switch usage.UsageSource {
		case "estimated":
			return usageBillingPathLocal
		case "provider_and_estimated":
			return usageBillingPathMixed
		}
	}
	effectiveUsage, ok := usageFromBillingUsage(usage)
	if !ok {
		if isLocalCountTokens {
			return usageBillingPathLocal
		}
		return usageBillingPathUpstream
	}

	switch effectiveUsage.UsageSemantic {
	case dto.BillingUsageSemanticOpenAI:
		if usage.BillingUsage.Estimated {
			return usageBillingPathOpenAIEstimated
		}
		return usageBillingPathOpenAI
	case dto.BillingUsageSemanticAnthropic:
		if usage.BillingUsage.Estimated {
			return usageBillingPathAnthropicEstimated
		}
		return usageBillingPathAnthropic
	case dto.BillingUsageSemanticGemini:
		if usage.BillingUsage.Estimated {
			return usageBillingPathGeminiEstimated
		}
		return usageBillingPathGemini
	}

	return usageBillingPathUpstream
}

func appendUsageBillingPathForLog(other map[string]interface{}, isLocalCountTokens bool, usage *dto.Usage) {
	if other == nil {
		return
	}
	adminInfo, ok := other["admin_info"].(map[string]interface{})
	if !ok || adminInfo == nil {
		adminInfo = make(map[string]interface{})
		other["admin_info"] = adminInfo
	}
	adminInfo["usage_billing_path"] = usageBillingPathForLog(isLocalCountTokens, usage)
}

func usageFromBillingUsage(usage *dto.Usage) (*dto.Usage, bool) {
	if usage == nil || usage.BillingUsage == nil {
		return nil, false
	}
	billingUsage := usage.BillingUsage
	source := strings.TrimSpace(billingUsage.Source)
	semantic := strings.TrimSpace(billingUsage.Semantic)

	if billingUsage.OpenAIUsage != nil &&
		(strings.EqualFold(source, dto.BillingUsageSourceOAIChat) ||
			strings.EqualFold(source, dto.BillingUsageSourceOAIResponses) ||
			strings.EqualFold(semantic, dto.BillingUsageSemanticOpenAI)) {
		return usageFromOpenAIBillingUsage(billingUsage), true
	}

	if billingUsage.ClaudeUsage != nil &&
		(strings.EqualFold(source, dto.BillingUsageSourceClaudeMessages) ||
			strings.EqualFold(semantic, dto.BillingUsageSemanticAnthropic)) {
		return usageFromClaudeBillingUsage(billingUsage), true
	}

	if billingUsage.GeminiUsageMetadata != nil &&
		(strings.EqualFold(source, dto.BillingUsageSourceGeminiChat) ||
			strings.EqualFold(semantic, dto.BillingUsageSemanticGemini)) {
		return usageFromGeminiBillingUsage(billingUsage), true
	}

	return nil, false
}

func usageFromOpenAIBillingUsage(billingUsage *dto.BillingUsage) *dto.Usage {
	usage := *billingUsage.OpenAIUsage
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails = usage.InputTokensDetails.Clone()
	}
	if usage.OutputTokensDetails != nil {
		usage.CompletionTokenDetails = *usage.OutputTokensDetails
	}
	if usage.PromptTokens == 0 && usage.InputTokens > 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.CompletionTokens == 0 && usage.OutputTokens > 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokens == 0 && usage.PromptTokens > 0 {
		usage.InputTokens = usage.PromptTokens
	}
	if usage.OutputTokens == 0 && usage.CompletionTokens > 0 {
		usage.OutputTokens = usage.CompletionTokens
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	usage.UsageSemantic = dto.BillingUsageSemanticOpenAI
	usage.UsageSource = billingUsage.Source
	usage.BillingUsage = dto.CloneBillingUsage(billingUsage)
	return &usage
}

func usageFromClaudeBillingUsage(billingUsage *dto.BillingUsage) *dto.Usage {
	claudeUsage := billingUsage.ClaudeUsage
	cacheCreation5m := claudeUsage.GetCacheCreation5mTokens()
	if cacheCreation5m == 0 {
		cacheCreation5m = claudeUsage.ClaudeCacheCreation5mTokens
	}
	cacheCreation1h := claudeUsage.GetCacheCreation1hTokens()
	if cacheCreation1h == 0 {
		cacheCreation1h = claudeUsage.ClaudeCacheCreation1hTokens
	}

	usage := &dto.Usage{
		PromptTokens:                claudeUsage.InputTokens,
		CompletionTokens:            claudeUsage.OutputTokens,
		TotalTokens:                 claudeUsage.InputTokens + claudeUsage.OutputTokens,
		InputTokens:                 claudeUsage.InputTokens + claudeUsage.CacheReadInputTokens + claudeUsage.CacheCreationInputTokens,
		OutputTokens:                claudeUsage.OutputTokens,
		UsageSemantic:               dto.BillingUsageSemanticAnthropic,
		UsageSource:                 dto.BillingUsageSourceClaudeMessages,
		BillingUsage:                dto.CloneBillingUsage(billingUsage),
		ClaudeCacheCreation5mTokens: cacheCreation5m,
		ClaudeCacheCreation1hTokens: cacheCreation1h,
	}
	usage.PromptTokensDetails.CachedTokens = claudeUsage.CacheReadInputTokens
	usage.PromptTokensDetails.CachedCreationTokens = claudeUsage.CacheCreationInputTokens
	return usage
}

func usageFromGeminiBillingUsage(billingUsage *dto.BillingUsage) *dto.Usage {
	metadata := *billingUsage.GeminiUsageMetadata
	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	usage := &dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
		UsageSemantic:    dto.BillingUsageSemanticGemini,
		UsageSource:      dto.BillingUsageSourceGeminiChat,
		BillingUsage:     dto.CloneBillingUsage(billingUsage),
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		addGeminiInputTokenDetail(&usage.PromptTokensDetails, detail)
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		addGeminiInputTokenDetail(&usage.PromptTokensDetails, detail)
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	} else if usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}
	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}
	return usage
}

func addGeminiInputTokenDetail(details *dto.InputTokenDetails, detail dto.GeminiPromptTokensDetails) {
	switch detail.Modality {
	case "AUDIO":
		details.AudioTokens += detail.TokenCount
	case "IMAGE":
		details.ImageTokens += detail.TokenCount
	case "TEXT":
		details.TextTokens += detail.TokenCount
	}
}
