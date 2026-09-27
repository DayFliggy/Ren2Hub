package service

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// ToolSurchargeItem is one billable tool-call line for consume logs.
type ToolSurchargeItem struct {
	Name        string `json:"name"`
	Count       int    `json:"count"`
	Price       string `json:"price"`
	SubtotalUSD string `json:"subtotal_usd"`
}

func appendToolSurchargeLogInfo(other map[string]interface{}, items []ToolSurchargeItem) {
	if len(items) == 0 {
		return
	}
	other["tool_surcharges"] = items
}

type textQuotaSummary struct {
	PricingError           error
	PromptTokens           int
	CompletionTokens       int
	TotalTokens            int
	CacheTokens            int
	CacheCreationTokens    int
	CacheCreationTokens5m  int
	CacheCreationTokens1h  int
	ImageTokens            int
	AudioTokens            int
	ModelName              string
	TokenName              string
	UseTimeSeconds         int64
	ChannelMultiplier      decimal.Decimal
	Quota                  int
	IsClaudeUsageSemantic  bool
	UsageSemantic          string
	ToolSurchargeItems     []ToolSurchargeItem
	ToolCallSurchargeQuota decimal.Decimal
	DeliveredPerCall       bool
}

// hasBillableUsage reports whether this request should incur any charge.
// A request can carry zero tokens yet still be billable via a tool-call
// surcharge (e.g. /v1/alpha/search returns no usage but bills one web_search
// call), so token count alone is not sufficient to decide.
func (s *textQuotaSummary) hasBillableUsage() bool {
	return s.TotalTokens > 0 || s.DeliveredPerCall || !s.ToolCallSurchargeQuota.IsZero()
}

func cacheWriteTokensTotal(summary textQuotaSummary) int {
	if summary.CacheCreationTokens5m > 0 || summary.CacheCreationTokens1h > 0 {
		splitCacheWriteTokens := summary.CacheCreationTokens5m + summary.CacheCreationTokens1h
		if summary.CacheCreationTokens > splitCacheWriteTokens {
			return summary.CacheCreationTokens
		}
		return splitCacheWriteTokens
	}
	return summary.CacheCreationTokens
}

// Keep the original evidence until scope checks complete, then use canonical
// provider counters for the charge, affinity observation and logging.
func calculateTextQuotaSummary(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage) textQuotaSummary {
	summary := textQuotaSummary{ModelName: relayInfo.BillingModelName(), TokenName: ctx.GetString("token_name"),
		UseTimeSeconds: time.Now().Unix() - relayInfo.StartTime.Unix(),
		UsageSemantic:  usageSemanticFromUsage(relayInfo, usage), DeliveredPerCall: relayInfo.PriceData.Quote != nil && (relayInfo.PriceData.UsePrice || relayInfo.PriceData.Quote.PriceVersion.FlatFee != "") && relayInfo.HasValidOutput()}
	if relayInfo.PriceData.Quote == nil {
		summary.PricingError = fmt.Errorf("frozen billing quote is required")
		return summary
	}
	if usage == nil {
		usage = &dto.Usage{PromptTokens: relayInfo.GetEstimatePromptTokens(), UsageSource: "estimated"}
	}
	facts := NormalizedBillingUsage(usage, summary.UsageSemantic)
	if quote := relayInfo.PriceData.Quote; quote != nil && quote.PriceVersion.UsageScope == billing.UsageScopeEmbedding && embeddingHasOutputUsage(usage) {
		facts.Invalid = true
	}
	usage = effectiveBillingUsage(usage)
	summary.UsageSemantic = usageSemanticFromUsage(relayInfo, usage)
	summary.IsClaudeUsageSemantic = summary.UsageSemantic == "anthropic"
	summary.PromptTokens = usage.PromptTokens
	summary.CompletionTokens = usage.CompletionTokens
	summary.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	summary.CacheTokens = usage.PromptTokensDetails.CachedTokens
	summary.CacheCreationTokens = usage.PromptTokensDetails.CacheCreationTokensTotal()
	summary.CacheCreationTokens5m = usage.ClaudeCacheCreation5mTokens
	summary.CacheCreationTokens1h = usage.ClaudeCacheCreation1hTokens
	summary.ImageTokens = usage.PromptTokensDetails.ImageTokens
	summary.AudioTokens = usage.PromptTokensDetails.AudioTokens
	if err := collectBillingQuantities(ctx, relayInfo, &facts); err != nil {
		summary.PricingError = err
		return summary
	}
	if contract := relayInfo.PriceData.Quote.PriceVersion.Metering; contract != nil && contract.Source == "request_fixed" {
		facts.Quantities[contract.Quantity] = fmt.Sprint(relayInfo.PriceData.Quote.Attributes["quantity"])
	}
	if summary.DeliveredPerCall {
		facts.Quantities["calls"] = "1"
	}
	if relayInfo.PriceData.Usage != nil && relayInfo.PriceData.Usage.Complete && !relayInfo.PriceData.Usage.Estimated && relayInfo.PriceData.Usage.Source == "provider_image_count" && relayInfo.HasValidOutput() {
		for name, value := range relayInfo.PriceData.Usage.Quantities {
			if name == "images" {
				facts.Quantities[name] = value
			}
		}
	}
	if previous := relayInfo.PriceData.Usage; previous != nil && previous.Source == "provider_image_count" && previous.Complete && !previous.Estimated {
		onlyQuantities := relayInfo.PriceData.Quote != nil && imageOrCallOnlyPrice(relayInfo.PriceData.Quote.PriceVersion)
		if onlyQuantities {
			facts.Source = "provider_image_count"
			facts.Complete = true
			facts.Estimated = false
		}
	}
	if relayInfo.PriceData.Quote != nil && (imageOrCallOnlyPrice(relayInfo.PriceData.Quote.PriceVersion) || relayInfo.PriceData.Quote.PriceVersion.Metering != nil || relayInfo.PriceData.Quote.PriceVersion.Mode == billing.ModeFree) && TextUsageReadyForSettlement(ctx, relayInfo, usage) {
		// Only business completion and any explicitly metered quantities are
		// required here. Missing token counters are irrelevant to a fixed fee.
		source := "business_completion"
		if contract := relayInfo.PriceData.Quote.PriceVersion.Metering; contract != nil && contract.Source == "completion_fact" {
			source = facts.Source
		}
		if facts.Estimated || !facts.Complete || facts.Invalid {
			facts = billing.Usage{Source: source, Terminal: "completed", Complete: true, Quantities: facts.Quantities}
			summary.PromptTokens, summary.CompletionTokens, summary.TotalTokens = 0, 0, 0
		}
		summary.DeliveredPerCall = true
	}
	facts.ActualServiceTier = relayInfo.ActualServiceTier
	if facts.Complete && !facts.Estimated && !facts.Invalid && TextUsageReadyForSettlement(ctx, relayInfo, usage) {
		facts.Terminal = "completed"
	}
	relayInfo.PriceData.Usage = &facts
	result, err := billing.Calculate(relayInfo.PriceData.Quote, facts)
	if err != nil {
		summary.PricingError = err
		return summary
	}
	relayInfo.PriceData.Charge = result
	summary.ChannelMultiplier, summary.PricingError = decimal.NewFromString(relayInfo.PriceData.Quote.ChannelRatio)
	if summary.PricingError != nil {
		return summary
	}
	summary.Quota, summary.PricingError = billing.RequestQuota(result.FinalQuota)
	summary.TotalTokens = int(facts.ContextTokens + facts.OutputTokens)
	for _, value := range facts.Quantities {
		quantity, _ := decimal.NewFromString(value)
		summary.DeliveredPerCall = summary.DeliveredPerCall || quantity.IsPositive()
	}
	for _, line := range result.Lines {
		if !strings.HasPrefix(line.Quantity, "tool:") {
			continue
		}
		count, _ := decimal.NewFromString(line.Count)
		price, _ := decimal.NewFromString(line.UnitPrice)
		if line.Unit == "per_call" {
			price = price.Mul(decimal.NewFromInt(1000))
		}
		subtotal, _ := decimal.NewFromString(line.SubtotalUSD)
		subtotal = subtotal.Mul(summary.ChannelMultiplier)
		summary.ToolSurchargeItems = append(summary.ToolSurchargeItems, ToolSurchargeItem{Name: strings.TrimPrefix(line.Quantity, "tool:"), Count: int(count.IntPart()), Price: price.String(), SubtotalUSD: subtotal.String()})
		summary.ToolCallSurchargeQuota = summary.ToolCallSurchargeQuota.Add(subtotal.Mul(decimal.NewFromInt(billing.QuotaPerUSD)))
	}
	return summary
}

func collectBillingQuantities(ctx *gin.Context, info *relaycommon.RelayInfo, usage *billing.Usage) error {
	if info.BillingToolUsageInvalid {
		return fmt.Errorf("tool usage is missing a stable identity")
	}
	counts := map[string]int{}
	add := func(name string, count int) error {
		if count < 0 || count > math.MaxInt32 || counts[name] > math.MaxInt32-count {
			return fmt.Errorf("invalid tool usage quantity")
		}
		counts[name] += count
		return nil
	}
	if info.ResponsesUsageInfo != nil {
		for name, tool := range info.ResponsesUsageInfo.BuiltInTools {
			if tool != nil {
				if err := add(name, tool.CallCount); err != nil {
					return err
				}
			}
		}
	}
	if info.RelayMode != relayconstant.RelayModeResponses && strings.HasSuffix(info.BillingModelName(), "search-preview") {
		if err := add(dto.BuildInToolWebSearchPreview, 1); err != nil {
			return err
		}
	}
	if err := add(dto.BuildInToolWebSearch, ctx.GetInt("claude_web_search_requests")); err != nil {
		return err
	}
	if ctx.GetBool("gemini_google_search_call") {
		if err := add(dto.BuildInToolGoogleSearch, 1); err != nil {
			return err
		}
	}
	for name, count := range counts {
		if count != 0 {
			usage.Quantities["tool:"+name] = strconv.Itoa(count)
		}
	}
	return nil
}
func usageSemanticFromUsage(relayInfo *relaycommon.RelayInfo, usage *dto.Usage) string {
	if usage != nil && usage.UsageSemantic != "" {
		return usage.UsageSemantic
	}
	if relayInfo != nil && relayInfo.GetFinalRequestRelayFormat() == types.RelayFormatClaude {
		return "anthropic"
	}
	return "openai"
}

func imageOrCallOnlyPrice(version billing.PriceVersion) bool {
	if version.Mode == billing.ModeExpression {
		if version.UsageSchema != nil || version.NeedsTokenUsage() {
			return false
		}
	} else if version.Mode != billing.ModeUnitPrice || len(version.Prices) == 0 && version.FlatFee == "" {
		return false
	}
	for _, price := range version.Prices {
		if price.Quantity != "calls" && price.Quantity != "images" {
			return false
		}
	}
	return true
}

// TextUsageReadyForSettlement requires an explicit successful protocol terminal
// for a stream and actual provider counters for token-dependent prices.
func TextUsageReadyForSettlement(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) bool {
	if info == nil || info.PriceData.Quote == nil {
		return false
	}
	terminalKnown := !info.IsStream && info.HasValidOutput()
	// Binary audio also records a protocol terminal. Delivered bytes alone do
	// not confirm a completed request when that transport ended abnormally.
	if info.StreamStatus != nil {
		terminalKnown = info.StreamStatus.IsNormalEnd()
	}
	explicitTerminal := false
	if value, exists := ctx.Get("response_termination"); exists {
		if terminal, ok := value.(types.ResponseTermination); ok && terminal.Kind != "" {
			explicitTerminal = true
			terminalKnown = terminal.IsSuccessful()
		}
	}
	if info.IsStream && !explicitTerminal && !terminalKnown && info.StreamStatus != nil {
		terminalKnown = info.StreamStatus.IsNormalEnd()
	}
	if !terminalKnown {
		return false
	}
	version := info.PriceData.Quote.PriceVersion
	if version.Mode == billing.ModeFree {
		return true
	}
	if contract := version.Metering; contract != nil {
		var raw string
		var present bool
		if contract.Source == "request_fixed" {
			raw, present = info.PriceData.Quote.Attributes["quantity"].(string)
		} else if usage != nil {
			if strings.Contains(usage.UsageSource, "estimated") || usage.UsageSource == "missing" || usage.UsageSource == "partial" || usage.UsageSource == "invalid" {
				return false
			}
			if observation := usage.Observation; observation != nil && (!observation.Complete || observation.Estimated || observation.Terminal != "completed") {
				return false
			}
			raw, present = usage.MeasuredQuantities[contract.Quantity]
		}
		value, err := billing.ParseAmountUSD(raw)
		return present && err == nil && !value.IsNegative()
	}
	if version.NeedsTokenUsage() && usage != nil && (usage.UsageSource == "provider_duration" || usage.UsageSource == "provider_characters") {
		return false
	}
	quantityOnly := imageOrCallOnlyPrice(version)
	if version.UsesImageCount() {
		facts := info.PriceData.Usage
		if facts == nil || !facts.Complete || facts.Estimated || facts.Source != "provider_image_count" {
			return false
		}
		count, err := strconv.ParseInt(facts.Quantities["images"], 10, 64)
		if err != nil || count < 1 || count > int64(dto.MaxImageN) {
			return false
		}
	}
	if quantityOnly {
		if !info.HasValidOutput() {
			return false
		}
		for _, price := range version.Prices {
			if price.Quantity == "images" {
				facts := info.PriceData.Usage
				if facts == nil || !facts.Complete || facts.Estimated || facts.Source != "provider_image_count" {
					return false
				}
				count, err := strconv.ParseInt(facts.Quantities["images"], 10, 64)
				if err != nil || count < 0 || count > int64(dto.MaxImageN) {
					return false
				}
			}
		}
		return true
	}
	facts := NormalizedBillingUsage(usage, usageSemanticFromUsage(info, usage))
	return usage != nil && !facts.Estimated && facts.Complete && !facts.Invalid &&
		(usage.UsageSource != "" || dto.HasOpenAIUsageTokens(usage) || usage.BillingUsage != nil)
}

// PrepareFinalTextCharge lets administrative channel probes use the same
// protocol completion, actual usage and quantity handling as relay requests.
func PrepareFinalTextCharge(ctx *gin.Context, info *relaycommon.RelayInfo, usage *dto.Usage) (int, error) {
	if !TextUsageReadyForSettlement(ctx, info, usage) {
		return 0, fmt.Errorf("completed billing evidence required")
	}
	summary := calculateTextQuotaSummary(ctx, info, usage)
	return summary.Quota, summary.PricingError
}

func PostTextConsumeQuota(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, usage *dto.Usage, extraContent []string) {
	if !TextUsageReadyForSettlement(ctx, relayInfo, usage) {
		HoldUnconfirmedResponseUsage(ctx, relayInfo, usage)
		return
	}
	originUsage := usage
	billingUsage := effectiveBillingUsage(usage)
	if usage == nil {
		extraContent = append(extraContent, "上游无计费信息")
	}
	if originUsage != nil {
		ObserveChannelAffinityUsageCacheByRelayFormat(ctx, billingUsage, relayInfo.GetFinalRequestRelayFormat())
	}

	adminRejectReason := common.GetContextKeyString(ctx, constant.ContextKeyAdminRejectReason)
	summary := calculateTextQuotaSummary(ctx, relayInfo, originUsage)
	if summary.PricingError != nil {
		holdFailedBillingPrice(ctx, relayInfo, billingexpr.TokenParams{}, summary.PricingError)
		return
	}

	for _, item := range summary.ToolSurchargeItems {
		subtotal, err := decimal.NewFromString(item.SubtotalUSD)
		if err != nil {
			holdFailedBillingPrice(ctx, relayInfo, billingexpr.TokenParams{}, err)
			return
		}
		q := subtotal.Mul(decimal.NewFromInt(billing.QuotaPerUSD))
		extraContent = append(extraContent, fmt.Sprintf(
			"%s 调用 %d 次，调用花费 %s",
			item.Name,
			item.Count,
			logger.LogQuota(common.QuotaFromDecimal(q)),
		))
	}
	if !summary.hasBillableUsage() {
		extraContent = append(extraContent, "上游没有返回计费信息，无法扣费（可能是上游超时）")
		logger.LogError(ctx, fmt.Sprintf("total tokens is 0, cannot consume quota, userId %d, channelId %d, tokenId %d, model %s， pre-consumed quota %d", relayInfo.UserId, relayInfo.ChannelId, relayInfo.TokenId, summary.ModelName, relayInfo.FinalPreConsumedQuota))
	}

	logModel := summary.ModelName
	if strings.HasPrefix(logModel, "gpt-4-gizmo") {
		logModel = "gpt-4-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}
	if strings.HasPrefix(logModel, "gpt-4o-gizmo") {
		logModel = "gpt-4o-gizmo-*"
		extraContent = append(extraContent, fmt.Sprintf("模型 %s", summary.ModelName))
	}

	logContent := strings.Join(extraContent, ", ")
	var other map[string]interface{}
	if summary.IsClaudeUsageSemantic {
		other = GenerateClaudeOtherInfo(ctx, relayInfo, summary.CacheTokens,
			summary.CacheCreationTokens, summary.CacheCreationTokens5m, summary.CacheCreationTokens1h)
		other["usage_semantic"] = "anthropic"
	} else {
		other = GenerateTextOtherInfo(ctx, relayInfo, summary.CacheTokens)
	}
	appendUsageBillingPathForLog(other, common.GetContextKeyBool(ctx, constant.ContextKeyLocalCountTokens), originUsage)
	if adminRejectReason != "" {
		other["reject_reason"] = adminRejectReason
	}
	if summary.ImageTokens != 0 {
		other["image"] = true
		other["image_output"] = summary.ImageTokens
	}
	appendToolSurchargeLogInfo(other, summary.ToolSurchargeItems)
	if summary.CacheCreationTokens > 0 {
		other["cache_creation_tokens"] = summary.CacheCreationTokens
	}
	if summary.CacheCreationTokens5m > 0 {
		other["cache_creation_tokens_5m"] = summary.CacheCreationTokens5m
	}
	if summary.CacheCreationTokens1h > 0 {
		other["cache_creation_tokens_1h"] = summary.CacheCreationTokens1h
	}
	cacheWriteTokens := cacheWriteTokensTotal(summary)
	if cacheWriteTokens > 0 {
		// cache_write_tokens: normalized cache creation total for UI display.
		// If split 5m/1h values are present, this is their sum; otherwise it falls back
		// to cache_creation_tokens.
		other["cache_write_tokens"] = cacheWriteTokens
	}
	if relayInfo.GetFinalRequestRelayFormat() != types.RelayFormatClaude && billingUsage != nil && billingUsage.UsageSource != "" && billingUsage.InputTokens > 0 {
		// input_tokens_total: explicit normalized total input used by the usage log UI.
		// Only write this field when upstream/current conversion has already provided a
		// reliable total input value and tagged the usage source. Do not infer it from
		// prompt/cache fields here, otherwise old upstream payloads may be double-counted.
		other["input_tokens_total"] = billingUsage.InputTokens
	}
	InjectBillingPriceInfo(other, relayInfo)

	attachQuotaSaturation(ctx, relayInfo, other)

	settleBillingConsumption(ctx, relayInfo, model.RecordConsumeLogParams{
		ChannelId:        relayInfo.ChannelId,
		PromptTokens:     summary.PromptTokens,
		CompletionTokens: summary.CompletionTokens,
		ModelName:        logModel,
		TokenName:        summary.TokenName,
		Quota:            common.Quota(summary.Quota),
		Content:          logContent,
		TokenId:          relayInfo.TokenId,
		UseTimeSeconds:   int(summary.UseTimeSeconds),
		IsStream:         relayInfo.IsStream,
		Group:            relayInfo.UsingGroup,
		Other:            other,
	}, summary.hasBillableUsage())
	gopool.Go(func() {
		perfmetrics.RecordRelaySample(relayInfo, true, int64(summary.CompletionTokens))
	})
}
