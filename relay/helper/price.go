package helper

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const defaultTieredPreConsumeMaxTokens = 8192

const pinnedBillingPriceKey = "billing_pinned_price"

type pinnedBillingPrice struct {
	RequestID string
	Origin    string
	Prices    map[int]billing.PriceVersion
	FrozenAt  time.Time
}

// PinBillingPrice is called only after a durable dispatch record was saved.
// Each channel keeps its first dispatched version. Switching channels resolves
// a new scoped price while preserving the logical request's frozen clock.
func PinBillingPrice(c *gin.Context, info *relaycommon.RelayInfo) error {
	if info == nil || info.PriceData.Quote == nil {
		return fmt.Errorf("cannot pin missing billing quote")
	}
	quote := info.PriceData.Quote
	channelID := 0
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
	}
	pinned := pinnedBillingPrice{RequestID: info.RequestId, Origin: info.OriginModelName, Prices: map[int]billing.PriceVersion{}, FrozenAt: quote.FrozenAt}
	if value, exists := c.Get(pinnedBillingPriceKey); exists {
		var ok bool
		pinned, ok = value.(pinnedBillingPrice)
		if !ok || pinned.RequestID != info.RequestId || pinned.Origin != info.OriginModelName || !pinned.FrozenAt.Equal(quote.FrozenAt) {
			return fmt.Errorf("logical request billing identity changed")
		}
		if price, exists := pinned.Prices[channelID]; exists {
			if price.ID != quote.PriceVersion.ID || price.Hash != quote.PriceVersion.Hash {
				return fmt.Errorf("channel attempt billing identity changed")
			}
			return nil
		}
	}
	price, err := billing.PreparePriceVersion(quote.PriceVersion)
	if err != nil {
		return err
	}
	pinned.Prices[channelID] = price
	c.Set(pinnedBillingPriceKey, pinned)
	return nil
}

func modelPriceNotConfiguredError(modelName string, _ int) error {
	return fmt.Errorf("模型 %s 尚未发布明确单价或计费表达式 / Model price is not configured", modelName)
}

func HandleChannelRatio(ctx *gin.Context, info *relaycommon.RelayInfo) string {
	ratio := ""
	if ctx != nil {
		ratio = ctx.GetString("channel_ratio")
	}
	info.UsingGroup = ""
	return ratio
}

func priceHasQuantity(version billing.PriceVersion, name string) bool {
	for _, price := range version.Prices {
		if price.Quantity == name {
			return true
		}
	}
	return false
}

func isEmbeddingBillingRequest(info *relaycommon.RelayInfo) bool {
	if info.RelayMode == relayconstant.RelayModeEmbeddings {
		return true
	}
	if info.RelayMode != relayconstant.RelayModeGemini {
		return false
	}
	// Native Gemini keeps its protocol mode for endpoint and response mapping.
	switch info.Request.(type) {
	case *dto.GeminiEmbeddingRequest, *dto.GeminiBatchEmbeddingRequest:
		return true
	default:
		return false
	}
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	return modelPriceWithFacts(c, info, promptTokens, meta, nil)
}

func ModelPriceHelperWithTaskFacts(c *gin.Context, info *relaycommon.RelayInfo, facts map[string]any) (hosttypes.PriceData, error) {
	return modelPriceWithFacts(c, info, 0, &types.TokenCountMeta{}, facts)
}

func modelPriceWithFacts(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta, facts map[string]any) (hosttypes.PriceData, error) {
	var version *billing.PriceVersion
	var frozenAt time.Time
	var err error
	channelID := 0
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
	}
	if value, exists := c.Get(pinnedBillingPriceKey); exists {
		pinned, ok := value.(pinnedBillingPrice)
		if !ok || pinned.RequestID != info.RequestId || pinned.Origin != info.OriginModelName {
			return hosttypes.PriceData{}, fmt.Errorf("logical request billing identity changed")
		}
		frozenAt = pinned.FrozenAt
		if price, exists := pinned.Prices[channelID]; exists {
			version = &price
		}
	}
	if version == nil {
		for _, name := range reasoning.BillingModelCandidates(info.BillingModelName()) {
			version, err = model.GetActiveBillingPriceForChannel(c.Request.Context(), name, channelID)
			if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
				break
			}
		}
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, model.ErrAutomaticBillingPriceUnavailable) {
			return hosttypes.PriceData{}, modelPriceNotConfiguredError(info.BillingModelName(), info.UserId)
		}
		return hosttypes.PriceData{}, fmt.Errorf("billing price unavailable: %w", err)
	}
	if version == nil {
		return hosttypes.PriceData{}, modelPriceNotConfiguredError(info.BillingModelName(), info.UserId)
	}
	if version.UsageScope == billing.UsageScopeEmbedding && !isEmbeddingBillingRequest(info) {
		return hosttypes.PriceData{}, fmt.Errorf("embedding price cannot be used for this request type")
	}
	input, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if input.FrozenAt.IsZero() {
		input.FrozenAt = time.Now().UTC()
	}
	if !frozenAt.IsZero() {
		input.FrozenAt = frozenAt
	}
	ratio := HandleChannelRatio(c, info)
	if version.ScopeKind == billing.PriceScopeChannel {
		ratio = "1"
	}
	ratio, err = model.NormalizeChannelRatio(ratio, false)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	requestFacts, estimatedFacts := map[string]any{}, map[string]any{}
	if version.UsageSchema != nil {
		for key, value := range facts {
			field, exists := version.UsageSchema.Fields[key]
			if !exists {
				return hosttypes.PriceData{}, fmt.Errorf("undeclared plugin usage fact %q", key)
			}
			if field.Source == billingexpr.UsageRequestFixed {
				requestFacts[key] = value
			} else {
				estimatedFacts[key] = value
			}
		}
	}
	quote, err := billing.NewQuoteWithFacts(*version, ratio, input.Attributes, input.FrozenAt, requestFacts)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	output := defaultTieredPreConsumeMaxTokens
	if rule := model_setting.GetGrokSettings(); rule != nil {
		fee, err := billing.ParseAmountUSD(rule.ViolationDeductionAmount)
		if err != nil {
			return hosttypes.PriceData{}, fmt.Errorf("invalid violation fee rule")
		}
		quote.ViolationFeeEnabled = rule.ViolationDeductionEnabled
		quote.ViolationFeeUSD = fee.String()
		quote.Hash = ""
		quote.Hash, err = billing.Hash(quote)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	}
	if meta != nil && meta.MaxTokens != 0 {
		output = meta.MaxTokens
	}
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech, relayconstant.RelayModeEmbeddings, relayconstant.RelayModeModerations, relayconstant.RelayModeRerank, relayconstant.RelayModeAlphaSearch:
		output = 0
	}
	if isEmbeddingBillingRequest(info) {
		output = 0
	}
	usage := billing.Usage{InputTokens: int64(promptTokens), OutputTokens: int64(output), ContextTokens: int64(promptTokens), InputIncludesCache: true, Quantities: map[string]string{}, Source: "request_estimate", Estimated: true}
	if version.UsageSchema != nil {
		usage.InputTokens, usage.OutputTokens, usage.ContextTokens = 0, 0, 0
		usage.EstimatedFacts = estimatedFacts
	}
	if version.Mode == billing.ModeExpression && version.UsageSchema == nil && version.UsageScope != billing.UsageScopeEmbedding {
		usage.Quantities["calls"] = "1"
	}
	perCall := priceHasQuantity(*version, "calls") || version.FlatFee != ""
	reasoningPriced := priceHasQuantity(*version, "reasoning") || (version.Mode == billing.ModeExpression && billingexpr.UsedVars(version.Expression)["reasoning"])
	if version.Mode == billing.ModeUnitPrice && !priceHasQuantity(*version, "p") && !priceHasQuantity(*version, "c") && !reasoningPriced {
		usage.InputTokens = 0
		usage.OutputTokens = 0
	}
	if version.Mode == billing.ModeUnitPrice && reasoningPriced && !priceHasQuantity(*version, "c") {
		usage.ReasoningTokens = usage.OutputTokens
	}
	if perCall && version.UsageSchema == nil {
		usage.Quantities["calls"] = "1"
	}
	if version.UsageSchema == nil && (priceHasQuantity(*version, "images") || version.UsesImageCount()) {
		quantity := 1.0
		quantityKnown := false
		if value, ok := input.Attributes["quantity"].(float64); ok {
			quantity = value
			quantityKnown = true
		}
		if meta != nil {
			if n, ok := meta.BillingQuantities["n"]; ok {
				quantity = n
				quantityKnown = true
			}
		}
		if quantity < 1 || quantity > float64(dto.MaxImageN) || quantity != math.Trunc(quantity) || math.IsNaN(quantity) || math.IsInf(quantity, 0) || (version.UsesImageCount() && !quantityKnown) {
			return hosttypes.PriceData{}, fmt.Errorf("invalid image quantity")
		}
		usage.Quantities["images"] = decimal.NewFromFloat(quantity).String()
	}
	if version.UsageSchema == nil && priceHasQuantity(*version, "seconds") {
		duration, ok := input.Attributes["duration"]
		if !ok {
			return hosttypes.PriceData{}, fmt.Errorf("duration required for per-second price")
		}
		seconds, err := decimal.NewFromString(fmt.Sprint(duration))
		if err != nil {
			return hosttypes.PriceData{}, fmt.Errorf("invalid task duration")
		}
		if count, exists := input.Attributes["quantity"]; exists {
			quantity, err := decimal.NewFromString(fmt.Sprint(count))
			if err != nil || !quantity.Equal(quantity.Truncate(0)) {
				return hosttypes.PriceData{}, fmt.Errorf("invalid media quantity")
			}
			seconds = seconds.Mul(quantity)
		}
		usage.Quantities["seconds"] = seconds.String()
	}
	result, err := billing.Calculate(quote, usage)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if reasoningPriced && usage.ReasoningTokens == 0 && usage.OutputTokens > 0 {
		// Reserve the more expensive of plain output and reasoning output.
		// Actual provider counters remain authoritative at settlement.
		estimate := usage
		estimate.ReasoningTokens = estimate.OutputTokens
		reasoningResult, err := billing.Calculate(quote, estimate)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if reasoningResult.FinalQuota > result.FinalQuota {
			usage, result = estimate, reasoningResult
		}
	}
	quota, err := billing.RequestQuota(result.FinalQuota)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	free := version.Mode == billing.ModeFree
	if !free && quota == 0 {
		quota = 1
	}
	price := hosttypes.PriceData{Quote: quote, Usage: &usage, FreeModel: free, UsePrice: perCall, QuotaToPreConsume: quota, Quota: quota}
	info.PricingModelName = version.Model
	info.ModelPricingSource = "published_price_version"
	info.PriceData = price
	info.BillingRequestInput = &input
	return price, nil
}

func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	return ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
}

func HasModelBillingConfig(modelName string) bool {
	for _, name := range reasoning.BillingModelCandidates(modelName) {
		_, err := model.GetActiveBillingPrice(context.Background(), name)
		if err == nil {
			return true
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false
		}
	}
	return false
}
func HasExplicitModelBillingConfig(modelName string) bool { return HasModelBillingConfig(modelName) }
