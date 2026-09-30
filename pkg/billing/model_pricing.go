package billing

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"
	"github.com/tidwall/gjson"
)

// PricingValues contains one resolved NewAPI model configuration. Configuration
// lookup (including model-family defaults) belongs to the host, never settlement.
type PricingValues map[string]any

func NewModelPriceVersion(model string, effective PricingValues, taskUsage bool) (PriceVersion, error) {
	values := make(PricingValues, len(effective))
	for key, value := range effective {
		values[key] = value
	}
	version := PriceVersion{Model: model, ScopeKind: PriceScopeGlobal, ContractVersion: ContractVersion, CalculationVersion: CalculationVersion, ResultUnit: ResultUSD, Pricing: values, TaskUsage: taskUsage}
	if values["billing_setting.billing_mode"] == "tiered_expr" {
		version.Mode = ModeExpression
		version.Expression, _ = values["billing_setting.billing_expr"].(string)
		if !taskUsage {
			version.ResultUnit = ResultMicroUSD
		}
	} else if _, exists := values["ModelPrice"]; exists {
		version.Mode = ModePerCall
	} else {
		version.Mode = ModeRatio
	}
	if err := validateModelPriceVersion(version); err != nil {
		return PriceVersion{}, err
	}
	return version, nil
}

func pricingNumber(values PricingValues, key string, fallback float64) (decimal.Decimal, error) {
	value, exists := values[key]
	if !exists {
		return decimal.NewFromFloat(fallback), nil
	}
	var text string
	switch number := value.(type) {
	case string:
		text = number
	case float64:
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return decimal.Zero, fmt.Errorf("invalid model pricing number")
		}
		text = strconv.FormatFloat(number, 'g', -1, 64)
	default:
		text = fmt.Sprint(value)
	}
	number, err := decimal.NewFromString(text)
	if err != nil || number.IsNegative() {
		return decimal.Zero, fmt.Errorf("invalid model pricing number")
	}
	return number, nil
}

func validateModelPriceVersion(version PriceVersion) error {
	if version.Hash != "" {
		definition := version
		definition.ID, definition.CreatedAt, definition.Hash = 0, 0, ""
		hash, err := Hash(definition)
		if err != nil || hash != version.Hash {
			return fmt.Errorf("model pricing configuration hash mismatch")
		}
	}
	if version.ContractVersion != ContractVersion || version.CalculationVersion != CalculationVersion {
		return fmt.Errorf("unsupported model pricing contract")
	}
	if strings.TrimSpace(version.Model) == "" || len(version.Model) > 255 {
		return fmt.Errorf("invalid billing model")
	}
	if version.ScopeKind != PriceScopeGlobal || version.ChannelID != 0 {
		return fmt.Errorf("model pricing requires global scope")
	}
	seenToolPrices := make(map[string]bool, len(version.Prices))
	for _, price := range version.Prices {
		if !strings.HasPrefix(price.Quantity, "tool:") {
			return fmt.Errorf("only explicit tool surcharges may accompany model pricing")
		}
		if seenToolPrices[price.Quantity] || !quantityUnitAllowed(price.Quantity, price.Unit) {
			return fmt.Errorf("invalid or duplicate tool price")
		}
		seenToolPrices[price.Quantity] = true
		if _, err := positiveDecimal(price.Price); err != nil {
			return err
		}
		if _, err := unitDivisor(price.Unit); err != nil {
			return err
		}
	}
	for _, key := range []string{"ModelRatio", "ModelPrice", "CompletionRatio", "CacheRatio", "CreateCacheRatio", "ImageRatio", "AudioRatio", "AudioCompletionRatio"} {
		if _, err := pricingNumber(version.Pricing, key, 1); err != nil {
			return err
		}
	}
	switch version.Mode {
	case ModeExpression:
		if version.Pricing["billing_setting.billing_mode"] != "tiered_expr" || version.Pricing["billing_setting.billing_expr"] != version.Expression {
			return fmt.Errorf("model expression configuration mismatch")
		}
		if _, err := billingexpr.CompileWithUsageSchema(version.Expression, version.UsageSchema); err != nil {
			return fmt.Errorf("invalid model billing expression")
		}
		want := ResultMicroUSD
		if version.TaskUsage {
			if billingexpr.UsesFixedPricing(version.Expression) {
				return fmt.Errorf("fixed pricing is not supported for task usage expressions")
			}
			want = ResultUSD
		}
		if version.ResultUnit != want {
			return fmt.Errorf("model expression result unit mismatch")
		}
	case ModePerCall:
		if _, ok := version.Pricing["ModelPrice"]; !ok {
			return fmt.Errorf("model price is not configured")
		}
	case ModeRatio:
		if _, ok := version.Pricing["ModelRatio"]; !ok {
			return fmt.Errorf("model price is not configured")
		}
	default:
		return fmt.Errorf("unknown model pricing mode")
	}
	return nil
}

func calculateModelPricing(quote *Quote, usage Usage) (*ChargeResult, error) {
	version := quote.PriceVersion
	result := &ChargeResult{CalculationVersion: CalculationVersion, RoundingVersion: "newapi-v1", Lines: []ChargeLine{}}
	ratio, err := positiveDecimal(quote.ChannelRatio)
	if err != nil {
		return nil, err
	}
	var usd decimal.Decimal
	var expressionQuota float64
	if version.Mode == ModeExpression {
		params := billingexpr.TokenParams{}
		if !version.TaskUsage {
			params, err = usage.TokenParams(billingexpr.UsedVars(version.Expression))
			if err != nil {
				return nil, err
			}
		}
		input := billingexpr.RequestInput{}
		if usage.requestInput != nil {
			input = *usage.requestInput
		}
		if usage.environmentCalls != nil {
			input.Evaluation = billingexpr.ReplayEvaluation(usage.environmentCalls)
		}
		cost, trace, err := billingexpr.RunExprWithRequest(version.Expression, params, input)
		if err != nil {
			return nil, fmt.Errorf("billing expression evaluation failed")
		}
		usd = decimal.NewFromFloat(cost)
		expressionQuota = cost
		if !version.TaskUsage {
			usd = usd.Shift(-6)
			expressionQuota /= 1000000
		}
		expressionQuota *= float64(QuotaPerUSD)
		trace, err = billingexpr.PublicTrace(version.Expression, trace)
		if err != nil {
			return nil, fmt.Errorf("billing expression trace validation failed")
		}
		result.Trace = &trace
		result.ExpressionVersion = billingexpr.ExprVersion(version.Expression)
	} else if version.Mode == ModePerCall {
		usd, err = pricingNumber(version.Pricing, "ModelPrice", 0)
		if err != nil {
			return nil, err
		}
		if quantity, ok := usage.Quantities["images"]; ok {
			count, err := positiveDecimal(quantity)
			if err != nil || !count.IsInteger() || count.LessThan(decimal.NewFromInt(1)) || count.GreaterThan(decimal.NewFromInt(128)) {
				return nil, fmt.Errorf("invalid image billing count")
			}
			usd = usd.Mul(count)
			modelName, size, quality := version.Model, "", ""
			if usage.requestInput != nil {
				body := usage.requestInput.Body
				if model := gjson.GetBytes(body, "model"); model.Exists() {
					modelName = model.String()
				}
				size = gjson.GetBytes(body, "size").String()
				quality = gjson.GetBytes(body, "quality").String()
			}
			usd = usd.Mul(decimal.NewFromFloat(LegacyImagePriceRatio(modelName, size, quality)))
		}
	} else {
		// NewAPI's legacy ratio path always prices cache and image subcategories.
		// Unlike expression pricing, these factors are not opt-in identifiers.
		params, err := usage.TokenParams(map[string]bool{"cr": true, "cc": true, "cc1h": true, "img": true})
		if err != nil {
			return nil, err
		}
		modelRatio, _ := pricingNumber(version.Pricing, "ModelRatio", 0)
		completion, _ := pricingNumber(version.Pricing, "CompletionRatio", 1)
		cache, _ := pricingNumber(version.Pricing, "CacheRatio", 1)
		creation, _ := pricingNumber(version.Pricing, "CreateCacheRatio", 1.25)
		image, _ := pricingNumber(version.Pricing, "ImageRatio", 1)
		weighted := decimal.NewFromInt(params.P).
			Add(decimal.NewFromInt(params.C).Mul(completion)).
			Add(decimal.NewFromInt(params.CR).Mul(cache)).
			Add(decimal.NewFromInt(params.CC).Mul(creation)).
			Add(decimal.NewFromInt(params.CC1h).Mul(creation).Mul(decimal.NewFromFloat(1.6))).
			Add(decimal.NewFromInt(params.Img).Mul(image))
		if usage.PricingKind == "audio" || usage.PricingKind == "realtime" {
			audio, _ := pricingNumber(version.Pricing, "AudioRatio", 1)
			audioCompletion, _ := pricingNumber(version.Pricing, "AudioCompletionRatio", 1)
			weighted = decimal.NewFromInt(max(usage.InputTokens-usage.AudioInputTokens, 0)).
				Add(decimal.NewFromInt(max(usage.OutputTokens-usage.AudioOutputTokens, 0)).Mul(completion)).
				Add(decimal.NewFromInt(usage.AudioInputTokens).Mul(audio)).
				Add(decimal.NewFromInt(usage.AudioOutputTokens).Mul(audio).Mul(audioCompletion))
		}
		if usage.PricingKind != "audio" && usage.PricingKind != "realtime" && quote.AudioInputPrice == "" {
			if _, explicit := version.Pricing["AudioRatio"]; explicit {
				audio, _ := pricingNumber(version.Pricing, "AudioRatio", 1)
				weighted = weighted.Sub(decimal.NewFromInt(min(params.P, usage.AudioInputTokens))).Add(decimal.NewFromInt(usage.AudioInputTokens).Mul(audio))
				if usage.AudioOutputTokens > 0 {
					audioCompletion, _ := pricingNumber(version.Pricing, "AudioCompletionRatio", 1)
					weighted = weighted.Sub(decimal.NewFromInt(min(params.C, usage.AudioOutputTokens)).Mul(completion)).Add(decimal.NewFromInt(usage.AudioOutputTokens).Mul(audio).Mul(audioCompletion))
				}
			}
		}
		if quote.AudioInputPrice != "" && usage.PricingKind != "audio" && usage.PricingKind != "realtime" {
			audioPrice, _ := positiveDecimal(quote.AudioInputPrice)
			if audioPrice.IsPositive() {
				weighted = weighted.Sub(decimal.NewFromInt(min(params.P, usage.AudioInputTokens)))
				usd = decimal.NewFromInt(usage.AudioInputTokens).Mul(audioPrice).Shift(-6)
			}
		}
		usd = usd.Add(weighted.Mul(modelRatio).Div(decimal.NewFromInt(QuotaPerUSD)))
	}
	modifierKeys := make([]string, 0, len(quote.OtherRatios))
	for key := range quote.OtherRatios {
		modifierKeys = append(modifierKeys, key)
	}
	sort.Strings(modifierKeys)
	for _, key := range modifierKeys {
		value := quote.OtherRatios[key]
		factor, _ := positiveDecimal(value)
		usd = usd.Mul(factor)
		expressionQuota *= factor.InexactFloat64()
	}
	if usage.Estimated && quote.PreConsumeMultiplier != "" && (version.Mode == ModeRatio || version.Mode == ModeExpression && result.Trace.BillingUnit == billingexpr.BillingUnitToken) {
		factor, _ := positiveDecimal(quote.PreConsumeMultiplier)
		usd = usd.Mul(factor)
		expressionQuota *= factor.InexactFloat64()
	}
	surcharge := decimal.Zero
	toolPrices := make(map[string]bool, len(version.Prices))
	for _, price := range version.Prices {
		toolPrices[price.Quantity] = true
	}
	for key, quantity := range usage.Quantities {
		if !strings.HasPrefix(key, "tool:") {
			continue
		}
		count, err := positiveDecimal(quantity)
		if err != nil || !count.IsInteger() || count.GreaterThan(decimal.NewFromInt(math.MaxInt32)) {
			return nil, fmt.Errorf("invalid tool usage")
		}
		if count.IsPositive() && !toolPrices[key] {
			return nil, fmt.Errorf("missing price for quantity %s", key)
		}
	}
	for _, price := range version.Prices {
		quantity := usage.Quantities[price.Quantity]
		if quantity == "" {
			continue
		}
		count, err := positiveDecimal(quantity)
		if err != nil {
			return nil, fmt.Errorf("invalid tool usage")
		}
		unitPrice, _ := positiveDecimal(price.Price)
		divisor, _ := unitDivisor(price.Unit)
		line := count.Mul(unitPrice).Div(divisor)
		surcharge = surcharge.Add(line)
		result.Lines = append(result.Lines, ChargeLine{Quantity: price.Quantity, Count: count.String(), Unit: price.Unit, UnitPrice: price.Price, SubtotalUSD: line.String()})
	}
	result.Lines = append(result.Lines, ChargeLine{Quantity: version.Mode, Count: "1", Unit: "per_call", UnitPrice: usd.String(), SubtotalUSD: usd.String()})
	usd = usd.Add(surcharge)
	result.BaseUSD = usd.String()
	total := usd.Mul(ratio)
	result.Adjustments = []ChargeAdjustment{{Kind: "channel_multiplier", Multiplier: ratio.String(), BaseUSD: usd.String(), AmountUSD: total.Sub(usd).String(), ResultUSD: total.String()}}
	unrounded := total.Mul(decimal.NewFromInt(QuotaPerUSD))
	if unrounded.IsNegative() || unrounded.GreaterThan(decimal.NewFromInt(math.MaxInt32)) {
		return nil, fmt.Errorf("charge outside request limit")
	}
	charged := unrounded.Truncate(0)
	if version.Mode == ModeExpression && surcharge.IsZero() {
		expressionQuota *= ratio.InexactFloat64()
		if math.IsNaN(expressionQuota) || math.IsInf(expressionQuota, 0) || expressionQuota < 0 || expressionQuota > math.MaxInt32 {
			return nil, fmt.Errorf("charge outside request limit")
		}
		unrounded = decimal.NewFromFloat(expressionQuota)
		charged = decimal.NewFromFloat(math.Round(expressionQuota))
	} else if version.Mode == ModeExpression {
		unrounded = decimal.NewFromFloat(expressionQuota).Mul(ratio).Add(surcharge.Mul(ratio).Mul(decimal.NewFromInt(QuotaPerUSD)))
		charged = unrounded.Truncate(0)
	}
	if version.Mode == ModeRatio && !ratio.IsZero() && unrounded.IsPositive() && charged.IsZero() {
		charged = decimal.NewFromInt(1)
	}
	result.TotalUSD = total.String()
	result.UnroundedQuota = unrounded.String()
	result.FinalQuota = common.Quota(charged.IntPart())
	result.RoundingDelta = charged.Sub(unrounded).String()
	return result, nil
}
