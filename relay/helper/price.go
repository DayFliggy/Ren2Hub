package helper

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
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
	if info.RequestId == "" {
		info.RequestId = common.NewRequestId()
	}
	info.BillingAttemptID = common.NewRequestId()
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
		if info.TaskPluginRef != nil {
			version, err = model.ResolveTaskModelBillingPrice(c.Request.Context(), info.BillingModelName(), info.UpstreamModelName, *info.TaskPluginRef)
		} else {
			version, err = activeRequestPrice(c.Request.Context(), info.BillingModelName(), channelID)
		}
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(info.BillingModelName(), info.UserId)
			}
			return hosttypes.PriceData{}, err
		}
		required := info.BillingRequirements
		if required == nil {
			value, err := PricingRequirementsFromBody(info, nil)
			if err != nil {
				return hosttypes.PriceData{}, err
			}
			required = &value
		}
		for _, dimension := range required.Dimensions {
			if strings.HasPrefix(dimension, "tool:") && !priceHasQuantity(*version, dimension) {
				return hosttypes.PriceData{}, &billing.PricingError{Code: "missing_price_dimension", Dimension: dimension}
			}
		}
		version, err = model.StoreBillingPriceEvidence(c.Request.Context(), *version)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
	}
	input, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if err = validateAudioMetering(info, *version); err != nil {
		return hosttypes.PriceData{}, err
	}
	if input.FrozenAt.IsZero() {
		input.FrozenAt = time.Now().UTC()
	}
	if !frozenAt.IsZero() {
		input.FrozenAt = frozenAt
	}
	ratio, err := model.NormalizeChannelRatio(HandleChannelRatio(c, info), false)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	quote, err := billing.NewQuote(*version, ratio, input.Attributes, input.FrozenAt)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	multiplier, err := operation_setting.InputPreConsumeMultiplier()
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	quote.PreConsumeMultiplier = strconv.FormatFloat(multiplier, 'g', -1, 64)
	if meta != nil && version.Mode != billing.ModeExpression {
		for name, value := range meta.BillingQuantities {
			if name == "prompt_extend" {
				if quote.OtherRatios == nil {
					quote.OtherRatios = map[string]string{}
				}
				quote.OtherRatios[name] = value
			}
		}
	}
	if rule := model_setting.GetGrokSettings(); rule != nil {
		fee, err := billing.ParseAmountUSD(rule.ViolationDeductionAmount)
		if err != nil {
			return hosttypes.PriceData{}, fmt.Errorf("invalid violation fee rule")
		}
		quote.ViolationFeeEnabled, quote.ViolationFeeUSD = rule.ViolationDeductionEnabled, fee.String()
	}
	quote.Hash = ""
	quote.Hash, err = billing.Hash(quote)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	usage := billing.Usage{InputTokens: int64(promptTokens), ContextTokens: int64(promptTokens), InputIncludesCache: true, Quantities: map[string]string{}, Source: "request_estimate", Estimated: true}
	if version.TaskUsage {
		input = billingexpr.RequestInput{FrozenAt: input.FrozenAt, Attributes: input.Attributes}
		input.Usage, err = service.TaskExpressionUsage(facts, version.UsageSchema)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		usage.EstimatedFacts = facts
	}
	if _, imageRequest := info.Request.(*dto.ImageRequest); imageRequest || input.ImageCount != nil {
		input, err = ResolveImageBillingRequestInput(c, info, input)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		usage.Quantities["images"] = strconv.Itoa(*input.ImageCount)
	}
	usage, result, err := model.EvaluateBilling(quote, usage, input, info.RequestId, info.BillingAttemptID, "estimate")
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	quota, err := billing.RequestQuota(result.FinalQuota)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	perCall := version.Mode == billing.ModePerCall || result.Trace != nil && result.Trace.BillingUnit == billingexpr.BillingUnitRequest
	price := hosttypes.PriceData{Quote: quote, Usage: &usage, Charge: result, UsePrice: perCall, QuotaToPreConsume: quota, Quota: quota}
	info.PricingModelName, info.ModelPricingSource = version.Model, "model_pricing"
	info.PriceData, info.BillingRequestInput = price, &input
	return price, nil
}

func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	return ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
}

// QualifyPublishedPrice only qualifies the route. A quote can be created only
// after adapter conversion and channel parameter overrides have finished.
func QualifyPublishedPrice(c *gin.Context, info *relaycommon.RelayInfo) (string, error) {
	channelID := 0
	if info.ChannelMeta != nil {
		channelID = info.ChannelId
	}
	var version *billing.PriceVersion
	var err error
	if info.TaskPluginRef != nil {
		version, err = model.ResolveTaskModelBillingPrice(c.Request.Context(), info.BillingModelName(), info.UpstreamModelName, *info.TaskPluginRef)
	} else {
		version, err = activeRequestPrice(c.Request.Context(), info.BillingModelName(), channelID)
	}
	if err != nil {
		return "", err
	}
	if version == nil {
		return "", modelPriceNotConfiguredError(info.BillingModelName(), info.UserId)
	}
	ratio := HandleChannelRatio(c, info)
	return model.NormalizeChannelRatio(ratio, false)
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

func activeRequestPrice(ctx context.Context, modelName string, channelID int) (*billing.PriceVersion, error) {
	var price *billing.PriceVersion
	var err error
	for _, name := range reasoning.BillingModelCandidates(modelName) {
		price, err = model.GetActiveBillingPriceForChannel(ctx, name, channelID)
		if err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
			return price, err
		}
	}
	return price, err
}
