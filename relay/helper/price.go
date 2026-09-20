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
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/model_setting"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const defaultTieredPreConsumeMaxTokens = 8192

func modelPriceNotConfiguredError(modelName string, _ int) error {
	return fmt.Errorf("模型 %s 尚未发布明确单价或计费表达式 / Model price is not configured", modelName)
}

func HandleChannelRatio(ctx *gin.Context, info *relaycommon.RelayInfo) float64 {
	ratio := 1.0
	if ctx != nil {
		if value, ok := ctx.Get("channel_ratio"); ok {
			ratio, _ = value.(float64)
		}
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

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	version, err := model.GetActiveBillingPrice(c.Request.Context(), info.BillingModelName())
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return hosttypes.PriceData{}, modelPriceNotConfiguredError(info.BillingModelName(), info.UserId)
		}
		return hosttypes.PriceData{}, fmt.Errorf("billing price unavailable: %w", err)
	}
	input, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	if input.FrozenAt.IsZero() {
		input.FrozenAt = time.Now().UTC()
	}
	ratio := HandleChannelRatio(c, info)
	if ratio < 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return hosttypes.PriceData{}, fmt.Errorf("invalid channel price ratio")
	}
	quote, err := billing.NewQuote(*version, decimal.NewFromFloat(ratio).String(), input.Attributes, input.FrozenAt)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	output := defaultTieredPreConsumeMaxTokens
	if rule := model_setting.GetGrokSettings(); rule != nil {
		if rule.ViolationDeductionAmount < 0 || math.IsNaN(rule.ViolationDeductionAmount) || math.IsInf(rule.ViolationDeductionAmount, 0) {
			return hosttypes.PriceData{}, fmt.Errorf("invalid violation fee rule")
		}
		quote.ViolationFeeEnabled = rule.ViolationDeductionEnabled
		quote.ViolationFeeUSD = decimal.NewFromFloat(rule.ViolationDeductionAmount).String()
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
	usage := billing.Usage{InputTokens: int64(promptTokens), OutputTokens: int64(output), ContextTokens: int64(promptTokens), InputIncludesCache: true, Quantities: map[string]string{}, Source: "request_estimate", Estimated: true}
	if version.Mode == billing.ModeExpression {
		usage.Quantities["calls"] = "1"
	}
	perCall := priceHasQuantity(*version, "calls")
	if version.Mode == billing.ModeUnitPrice && !priceHasQuantity(*version, "p") && !priceHasQuantity(*version, "c") {
		usage.InputTokens = 0
		usage.OutputTokens = 0
	}
	if perCall {
		usage.Quantities["calls"] = "1"
	}
	if priceHasQuantity(*version, "images") {
		quantity := 1.0
		if value, ok := input.Attributes["quantity"].(float64); ok {
			quantity = value
		}
		if meta != nil {
			if n, ok := meta.BillingRatios["n"]; ok {
				quantity = n
			}
		}
		if quantity < 0 || math.IsNaN(quantity) || math.IsInf(quantity, 0) {
			return hosttypes.PriceData{}, fmt.Errorf("invalid image quantity")
		}
		usage.Quantities["images"] = decimal.NewFromFloat(quantity).String()
	}
	if priceHasQuantity(*version, "seconds") {
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
	quota, err := billing.RequestQuota(result.FinalQuota)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	free := version.Mode == billing.ModeFree
	if !free && quota == 0 {
		quota = 1
	}
	price := hosttypes.PriceData{Quote: quote, Usage: &usage, FreeModel: free, UsePrice: perCall, ChannelRatio: ratio, QuotaToPreConsume: quota, Quota: quota}
	// These fields are display/adapter projections only. The calculator reads
	// exclusively the immutable quote; no ratio is a second source of price.
	for _, p := range version.Prices {
		if p.Quantity == "calls" {
			d, _ := decimal.NewFromString(p.Price)
			price.ModelPrice = d.InexactFloat64()
		}
	}
	info.PricingModelName = version.Model
	info.ModelPricingSource = "published_price_version"
	info.PriceData = price
	info.BillingRequestInput = &input
	info.TieredBillingSnapshot = nil
	if version.Mode == billing.ModeExpression {
		info.TieredBillingSnapshot = &billingexpr.BillingSnapshot{BillingMode: "tiered_expr", ModelName: version.Model, ExprString: version.Expression, ExprHash: billingexpr.ExprHashString(version.Expression), GroupRatio: ratio, EstimatedPromptTokens: promptTokens, EstimatedCompletionTokens: output, EstimatedQuotaAfterGroup: quota, QuotaPerUnit: float64(billing.QuotaPerUSD), ExprVersion: billingexpr.ExprVersion(version.Expression)}
	}
	return price, nil
}

func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	return ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
}

func HasModelBillingConfig(modelName string) bool {
	_, err := model.GetActiveBillingPrice(context.Background(), modelName)
	return err == nil
}
func HasExplicitModelBillingConfig(modelName string) bool { return HasModelBillingConfig(modelName) }
