// Package billing owns the reproducible pricing contract. It has no database,
// current configuration, provider, or wall-clock dependencies.
package billing

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/shopspring/decimal"
)

const (
	ModeUnitPrice       = "unit_price"
	ModeExpression      = "expression"
	ModeFree            = "free"
	CalculationVersion  = "billing-v2"
	ContractVersion     = 2
	ResultMicroUSD      = "micro_usd"
	ResultUSD           = "usd"
	RoundingVersion     = "half-up-positive-minimum-v1"
	QuotaPerUSD         = int64(500000)
	UsageScopeEmbedding = "embedding"
	PriceScopeGlobal    = "global_model"
	PriceScopeChannel   = "channel_model"
	TierModeVolume      = "volume"
	TierModeGraduated   = "graduated"
)

// UnitPrice is USD per declared unit. Quantity is a named usage dimension;
// quantities never come from arbitrary request paths.
type UnitPrice struct {
	Quantity string      `json:"quantity"`
	Unit     string      `json:"unit"`
	Price    string      `json:"price"`
	TierMode string      `json:"tier_mode,omitempty"`
	Tiers    []PriceTier `json:"tiers,omitempty"`
}

// PriceTier describes one rate band. UpTo is an inclusive cumulative limit;
// an empty UpTo is allowed only on the final tier and means no upper bound.
// Volume pricing selects one tier for the complete quantity, while graduated
// pricing charges each occupied band independently.
type PriceTier struct {
	UpTo      string `json:"up_to,omitempty"`
	UnitPrice string `json:"unit_price"`
}

type PriceVersion struct {
	ContractVersion    int                      `json:"contract_version"`
	CalculationVersion string                   `json:"calculation_version"`
	ResultUnit         string                   `json:"result_unit"`
	UsageSchema        *billingexpr.UsageSchema `json:"usage_schema,omitempty"`
	UsageScope         string                   `json:"usage_scope,omitempty"`
	ID                 int64                    `json:"id"`
	ScopeKind          string                   `json:"scope_kind"`
	ChannelID          int                      `json:"channel_id,omitempty"`
	Model              string                   `json:"model"`
	Origin             string                   `json:"origin,omitempty"`
	SourceModel        string                   `json:"source_model,omitempty"`
	SourceHash         string                   `json:"source_hash,omitempty"`
	Mode               string                   `json:"mode"`
	Prices             []UnitPrice              `json:"prices"`
	FlatFee            string                   `json:"flat_fee,omitempty"`
	Expression         string                   `json:"expression,omitempty"`
	CreatedAt          int64                    `json:"created_at"`
	EffectiveFrom      *time.Time               `json:"effective_from,omitempty"`
	EffectiveTo        *time.Time               `json:"effective_to,omitempty"`
	Hash               string                   `json:"hash"`
}

// UsesImageCount identifies the canonical image quantity owned by a v1
// expression, independently of its token dimensions and extra price lines.
func (version PriceVersion) UsesImageCount() bool {
	expressionVersion, _ := billingexpr.ParseExprVersion(version.Expression)
	return version.Mode == ModeExpression && expressionVersion == 1 && billingexpr.UsedVars(version.Expression)["image_count"]
}

type Quote struct {
	ContractVersion     int                    `json:"contract_version"`
	CalculationVersion  string                 `json:"calculation_version"`
	RequestFacts        map[string]interface{} `json:"request_facts,omitempty"`
	PriceVersion        PriceVersion           `json:"price_version"`
	ChannelRatio        string                 `json:"channel_ratio"`
	Attributes          map[string]interface{} `json:"attributes,omitempty"`
	FrozenAt            time.Time              `json:"frozen_at"`
	Hash                string                 `json:"hash"`
	ChargeKind          string                 `json:"charge_kind,omitempty"`
	SourceQuoteHash     string                 `json:"source_quote_hash,omitempty"`
	ViolationFeeEnabled bool                   `json:"violation_fee_enabled"`
	ViolationFeeUSD     string                 `json:"violation_fee_usd"`
}

// Quantities hold decimal counts for calls/images/seconds/tool:<name>. Tokens
// retain the canonical input semantics independent from protocol projections.
type Usage struct {
	EstimatedFacts       map[string]interface{} `json:"estimated_facts,omitempty"`
	ActualFacts          map[string]interface{} `json:"actual_facts,omitempty"`
	InputTokens          int64                  `json:"input_tokens"`
	OutputTokens         int64                  `json:"output_tokens"`
	ContextTokens        int64                  `json:"context_tokens"`
	CacheReadTokens      int64                  `json:"cache_read_tokens"`
	CacheWriteTokens     int64                  `json:"cache_write_tokens"`
	CacheWrite1hTokens   int64                  `json:"cache_write_1h_tokens"`
	ImageInputTokens     int64                  `json:"image_input_tokens"`
	ImageCacheReadTokens *int64                 `json:"image_cache_read_tokens,omitempty"`
	ImageOutputTokens    int64                  `json:"image_output_tokens"`
	AudioInputTokens     int64                  `json:"audio_input_tokens"`
	AudioOutputTokens    int64                  `json:"audio_output_tokens"`
	ReasoningTokens      int64                  `json:"reasoning_tokens"`
	InputIncludesCache   bool                   `json:"input_includes_cache"`
	Quantities           map[string]string      `json:"quantities,omitempty"`
	Source               string                 `json:"source"`
	Estimated            bool                   `json:"estimated"`
	Complete             bool                   `json:"complete"`
	Invalid              bool                   `json:"invalid,omitempty"`
}

type ChargeLine struct {
	Quantity    string `json:"quantity"`
	Count       string `json:"count"`
	Unit        string `json:"unit"`
	UnitPrice   string `json:"unit_price"`
	SubtotalUSD string `json:"subtotal_usd"`
}

type ChargeResult struct {
	Lines              []ChargeLine             `json:"lines"`
	TotalUSD           string                   `json:"total_usd"`
	UnroundedQuota     string                   `json:"unrounded_quota"`
	FinalQuota         common.Quota             `json:"final_quota"`
	RoundingDelta      string                   `json:"rounding_delta"`
	CalculationVersion string                   `json:"calculation_version"`
	RoundingVersion    string                   `json:"rounding_version"`
	ExpressionVersion  int                      `json:"expression_version,omitempty"`
	Trace              *billingexpr.TraceResult `json:"trace,omitempty"`
}

func Hash(value interface{}) (string, error) {
	data, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}

// ParseAmountUSD validates monetary configuration using the same exact decimal
// bounds as frozen unit prices. Request charge limits are checked after use.
func ParseAmountUSD(s string) (decimal.Decimal, error) {
	if len(s) == 0 || len(s) > 64 {
		return decimal.Zero, fmt.Errorf("invalid decimal price")
	}
	d, err := decimal.NewFromString(s)
	if err != nil || d.IsNegative() || d.Exponent() < -18 || d.Exponent() > 18 {
		return decimal.Zero, fmt.Errorf("invalid decimal price")
	}
	return d, nil
}

func positiveDecimal(s string) (decimal.Decimal, error) { return ParseAmountUSD(s) }

func unitDivisor(unit string) (decimal.Decimal, error) {
	switch unit {
	case "per_million_tokens":
		return decimal.NewFromInt(1000000), nil
	case "per_thousand_calls":
		return decimal.NewFromInt(1000), nil
	case "per_call", "per_image", "per_second":
		return decimal.NewFromInt(1), nil
	default:
		return decimal.Zero, fmt.Errorf("unknown billing unit %q", unit)
	}
}

func tokenDimension(name string) bool {
	switch name {
	case "p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao", "reasoning":
		return true
	}
	return false
}

func validateTiers(price UnitPrice) error {
	if price.TierMode == "" {
		if len(price.Tiers) != 0 {
			return fmt.Errorf("billing tiers require a tier mode")
		}
		if _, err := positiveDecimal(price.Price); err != nil {
			return err
		}
		return nil
	}
	if price.TierMode != TierModeVolume && price.TierMode != TierModeGraduated {
		return fmt.Errorf("unknown billing tier mode")
	}
	if price.Price != "" || len(price.Tiers) == 0 || len(price.Tiers) > 32 {
		return fmt.Errorf("tiered billing requires tiers and no base price")
	}
	previous := decimal.Zero
	for index, tier := range price.Tiers {
		if _, err := positiveDecimal(tier.UnitPrice); err != nil {
			return err
		}
		if tier.UpTo == "" {
			if index != len(price.Tiers)-1 {
				return fmt.Errorf("unbounded billing tier must be last")
			}
			continue
		}
		upTo, err := positiveDecimal(tier.UpTo)
		if err != nil || upTo.IsZero() || !upTo.GreaterThan(previous) || upTo.GreaterThan(decimal.NewFromInt(math.MaxInt32)) {
			return fmt.Errorf("invalid billing tier boundary")
		}
		previous = upTo
	}
	return nil
}

func validatePriceWindow(version PriceVersion, at time.Time) error {
	if (version.EffectiveFrom != nil && version.EffectiveFrom.IsZero()) || (version.EffectiveTo != nil && version.EffectiveTo.IsZero()) {
		return fmt.Errorf("billing price window has invalid time")
	}
	if version.EffectiveFrom != nil && version.EffectiveTo != nil && version.EffectiveFrom.After(*version.EffectiveTo) {
		return fmt.Errorf("billing price window is inverted")
	}
	if at.IsZero() {
		return fmt.Errorf("quote requires frozen time")
	}
	if version.EffectiveFrom != nil && at.Before(*version.EffectiveFrom) {
		return fmt.Errorf("billing price is not yet effective")
	}
	if version.EffectiveTo != nil && at.After(*version.EffectiveTo) {
		return fmt.Errorf("billing price has expired")
	}
	return nil
}

func finiteDecimalFromFloat(value float64) (decimal.Decimal, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return decimal.Zero, fmt.Errorf("invalid billing expression result")
	}
	converted := decimal.NewFromFloat(value)
	if converted.Exponent() < -18 || converted.Exponent() > 18 || len(converted.String()) > 64 {
		return decimal.Zero, fmt.Errorf("billing expression result exceeds decimal range")
	}
	return converted, nil
}

func addTieredCharge(result *ChargeResult, total *decimal.Decimal, price UnitPrice, count, divisor decimal.Decimal) error {
	if count.IsNegative() {
		return fmt.Errorf("invalid billing quantity")
	}
	if count.IsZero() {
		return nil
	}
	if price.TierMode == TierModeVolume {
		for _, tier := range price.Tiers {
			limitReached := tier.UpTo == ""
			if !limitReached {
				limit, _ := positiveDecimal(tier.UpTo)
				limitReached = count.LessThanOrEqual(limit)
			}
			if !limitReached {
				continue
			}
			rate, _ := positiveDecimal(tier.UnitPrice)
			subtotal := count.Mul(rate).DivRound(divisor, 48)
			*total = total.Add(subtotal)
			result.Lines = append(result.Lines, ChargeLine{Quantity: price.Quantity, Count: count.String(), Unit: price.Unit, UnitPrice: rate.String(), SubtotalUSD: subtotal.String()})
			return nil
		}
		return fmt.Errorf("billing quantity exceeds tier range")
	}
	previous := decimal.Zero
	for _, tier := range price.Tiers {
		band := count.Sub(previous)
		if !band.IsPositive() {
			break
		}
		if tier.UpTo != "" {
			limit, _ := positiveDecimal(tier.UpTo)
			if count.LessThan(limit) {
				band = count.Sub(previous)
			} else {
				band = limit.Sub(previous)
			}
			previous = limit
		}
		rate, _ := positiveDecimal(tier.UnitPrice)
		subtotal := band.Mul(rate).DivRound(divisor, 48)
		*total = total.Add(subtotal)
		result.Lines = append(result.Lines, ChargeLine{Quantity: price.Quantity, Count: band.String(), Unit: price.Unit, UnitPrice: rate.String(), SubtotalUSD: subtotal.String()})
		if tier.UpTo == "" || count.LessThanOrEqual(previous) {
			return nil
		}
	}
	return fmt.Errorf("billing quantity exceeds tier range")
}

func ValidatePriceVersion(version PriceVersion) error {
	if _, err := PreparePriceVersion(version); err != nil {
		return err
	}
	return nil
}

// PreparePriceVersion materializes the current contract only for a new draft.
// Persisted definitions use ValidateFrozenPriceVersion and never infer versions.
func PreparePriceVersion(version PriceVersion) (PriceVersion, error) {
	if version.ID != 0 || version.Hash != "" {
		if err := ValidateFrozenPriceVersion(version); err != nil {
			return version, err
		}
	}
	if version.ContractVersion == 0 && version.ID == 0 && version.Hash == "" {
		version.ContractVersion = ContractVersion
	}
	if version.CalculationVersion == "" && version.ID == 0 && version.Hash == "" {
		version.CalculationVersion = CalculationVersion
	}
	expressionVersion, _ := billingexpr.ParseExprVersion(version.Expression)
	wantUnit := ResultUSD
	if version.Mode == ModeExpression && expressionVersion == 1 {
		wantUnit = ResultMicroUSD
	}
	if version.ResultUnit == "" && version.ID == 0 && version.Hash == "" {
		version.ResultUnit = wantUnit
	}
	if version.ScopeKind == "" && version.ID == 0 && version.Hash == "" {
		version.ScopeKind = PriceScopeGlobal
	}
	if version.UsageSchema != nil {
		frozen, err := billingexpr.FreezeUsageSchema(version.UsageSchema)
		if err != nil {
			return version, err
		}
		version.UsageSchema = frozen
	}
	if err := ValidateFrozenPriceVersion(version); err != nil {
		return version, err
	}
	version.Prices = append(make([]UnitPrice, 0, len(version.Prices)), version.Prices...)
	for i := range version.Prices {
		version.Prices[i].Tiers = append([]PriceTier(nil), version.Prices[i].Tiers...)
	}
	if version.EffectiveFrom != nil {
		at := *version.EffectiveFrom
		version.EffectiveFrom = &at
	}
	if version.EffectiveTo != nil {
		at := *version.EffectiveTo
		version.EffectiveTo = &at
	}
	return version, nil
}

func ValidateFrozenPriceVersion(version PriceVersion) error {
	if version.ContractVersion != ContractVersion || version.CalculationVersion != CalculationVersion {
		return fmt.Errorf("unsupported billing price contract")
	}
	if version.UsageScope != "" && version.UsageScope != UsageScopeEmbedding {
		return fmt.Errorf("unsupported billing usage scope")
	}
	if (version.EffectiveFrom != nil && version.EffectiveFrom.IsZero()) || (version.EffectiveTo != nil && version.EffectiveTo.IsZero()) {
		return fmt.Errorf("billing price window has invalid time")
	}
	if version.EffectiveFrom != nil && version.EffectiveTo != nil && version.EffectiveFrom.After(*version.EffectiveTo) {
		return fmt.Errorf("billing price window is inverted")
	}
	expressionVersion, _ := billingexpr.ParseExprVersion(version.Expression)
	wantUnit := ResultUSD
	if version.Mode == ModeExpression && expressionVersion == 1 {
		wantUnit = ResultMicroUSD
	}
	if version.ResultUnit != wantUnit {
		return fmt.Errorf("billing expression result unit mismatch")
	}
	if version.UsageSchema != nil {
		if err := billingexpr.ValidateUsageSchema(version.UsageSchema); err != nil {
			return err
		}
	}
	if strings.TrimSpace(version.Model) == "" || len(version.Model) > 255 {
		return fmt.Errorf("invalid billing model")
	}
	if version.FlatFee != "" {
		if _, err := positiveDecimal(version.FlatFee); err != nil {
			return fmt.Errorf("invalid flat fee")
		}
	}
	switch version.ScopeKind {
	case PriceScopeGlobal:
		if version.ChannelID != 0 {
			return fmt.Errorf("global billing price cannot have a channel")
		}
	case PriceScopeChannel:
		if version.ChannelID <= 0 {
			return fmt.Errorf("channel billing price requires a channel")
		}
	default:
		return fmt.Errorf("unknown billing price scope")
	}
	switch version.Mode {
	case ModeFree:
		if len(version.Prices) != 0 || version.Expression != "" {
			return fmt.Errorf("free prices cannot contain charges")
		}
		if version.FlatFee != "" {
			fee, _ := positiveDecimal(version.FlatFee)
			if !fee.IsZero() {
				return fmt.Errorf("free prices cannot contain charges")
			}
		}
	case ModeExpression:
		if len(version.Expression) > 16384 {
			return fmt.Errorf("billing expression too long")
		}
		if _, err := billingexpr.CompileWithUsageSchema(version.Expression, version.UsageSchema); err != nil {
			return fmt.Errorf("invalid billing expression")
		}
	case ModeUnitPrice:
		if version.Expression != "" || (len(version.Prices) == 0 && version.FlatFee == "") {
			return fmt.Errorf("unit price requires prices or a flat fee and no expression")
		}
	default:
		return fmt.Errorf("unknown billing mode")
	}
	if version.UsageScope == UsageScopeEmbedding {
		if version.UsageSchema != nil || (version.Mode == ModeExpression && expressionVersion != 1) {
			return fmt.Errorf("embedding scope requires token pricing without a usage schema")
		}
		if version.Mode == ModeExpression {
			_, body := billingexpr.ParseExprVersion(version.Expression)
			tree, err := parser.Parse(body)
			if err != nil {
				return fmt.Errorf("invalid embedding expression")
			}
			used := map[string]bool{}
			// Validate every branch before optimization can remove zero rates
			// or unreachable output and fixed-price terms.
			ast.Find(tree.Node, func(node ast.Node) bool {
				if identifier, ok := node.(*ast.IdentifierNode); ok {
					used[identifier.Value] = true
				}
				return false
			})
			if !used["p"] || used["c"] || used["reasoning"] || used["img_o"] || used["ao"] || used["image_count"] || used["fixed"] {
				return fmt.Errorf("embedding scope requires an input-token expression")
			}
		}
	}
	seen := map[string]bool{}
	imageExpression := version.UsesImageCount()
	var expressionFields map[string]bool
	if version.Mode == ModeExpression && expressionVersion == 2 {
		var err error
		expressionFields, err = billingexpr.UsedUsageFields(version.Expression, version.UsageSchema)
		if err != nil {
			return err
		}
		for key := range expressionFields {
			if version.UsageSchema.Fields[key].Source == billingexpr.UsageEstimateOnly {
				return fmt.Errorf("estimate-only usage cannot define a final charge")
			}
		}
	}
	for _, price := range version.Prices {
		if version.UsageScope == UsageScopeEmbedding && (!tokenDimension(price.Quantity) || price.Quantity == "c" || price.Quantity == "reasoning" || price.Quantity == "img_o" || price.Quantity == "ao") {
			return fmt.Errorf("embedding scope only permits input-token prices")
		}
		if imageExpression && price.Quantity == "images" {
			return fmt.Errorf("expression already owns image quantity")
		}
		if len(price.Quantity) > 128 {
			return fmt.Errorf("billing quantity too long")
		}
		if version.Mode == ModeExpression && tokenDimension(price.Quantity) {
			return fmt.Errorf("expression already owns token prices")
		}
		if expressionFields[price.Quantity] {
			return fmt.Errorf("expression already owns usage quantity")
		}
		if seen[price.Quantity] {
			return fmt.Errorf("duplicate billing quantity")
		}
		seen[price.Quantity] = true
		if err := validateTiers(price); err != nil {
			return err
		}
		field, declared := billingexpr.UsageField{}, false
		if version.UsageSchema != nil {
			field, declared = version.UsageSchema.Fields[price.Quantity]
		}
		if declared {
			if field.Type != "number" || field.Source == billingexpr.UsageEstimateOnly || price.Unit != "per_"+field.Unit {
				return fmt.Errorf("schema price requires a final numeric quantity and matching unit")
			}
			continue
		}
		if version.UsageSchema != nil {
			return fmt.Errorf("price quantity is not declared in usage schema")
		}
		if _, err := unitDivisor(price.Unit); err != nil {
			return err
		}
		if tokenDimension(price.Quantity) {
			if price.Unit != "per_million_tokens" {
				return fmt.Errorf("token price requires token unit")
			}
		} else if strings.HasPrefix(price.Quantity, "tool:") && len(price.Quantity) > 5 {
			if price.Unit != "per_call" && price.Unit != "per_thousand_calls" {
				return fmt.Errorf("tool price requires call unit")
			}
		} else {
			expected := map[string]string{"calls": "per_call", "images": "per_image", "seconds": "per_second"}[price.Quantity]
			if expected == "" || price.Unit != expected {
				return fmt.Errorf("invalid billing quantity or unit")
			}
		}
	}
	if version.UsageScope == UsageScopeEmbedding && version.Mode == ModeUnitPrice && !seen["p"] {
		return fmt.Errorf("embedding scope requires an input-token price")
	}
	return nil
}

func NewQuote(version PriceVersion, ratio string, attributes map[string]interface{}, at time.Time) (*Quote, error) {
	return NewQuoteWithFacts(version, ratio, attributes, at, nil)
}

// NewViolationQuote preserves the published price identity and derives only
// the frozen adjustment. Reusing its ID for a different price would make the
// resulting settlement impossible to replay against that version.
func NewViolationQuote(original *Quote) (*Quote, error) {
	if original == nil || original.ChargeKind != "" || !original.ViolationFeeEnabled {
		return nil, fmt.Errorf("missing frozen violation fee rule")
	}
	copyQuote := *original
	copyQuote.Hash = ""
	hash, err := Hash(copyQuote)
	if err != nil || hash != original.Hash {
		return nil, fmt.Errorf("source quote integrity mismatch")
	}
	copyQuote.PriceVersion, err = PreparePriceVersion(original.PriceVersion)
	if err != nil {
		return nil, err
	}
	copyQuote.Attributes = make(map[string]interface{}, len(original.Attributes))
	for key, value := range original.Attributes {
		copyQuote.Attributes[key] = value
	}
	copyQuote.RequestFacts, err = freezeRequestFacts(original.PriceVersion.UsageSchema, original.RequestFacts)
	if err != nil {
		return nil, err
	}
	copyQuote.ChargeKind, copyQuote.SourceQuoteHash = "violation_fee", original.Hash
	copyQuote.Hash, err = Hash(copyQuote)
	if err != nil {
		return nil, err
	}
	return &copyQuote, nil
}

func NewQuoteWithFacts(version PriceVersion, ratio string, attributes map[string]interface{}, at time.Time, facts map[string]interface{}) (*Quote, error) {
	version, err := PreparePriceVersion(version)
	if err != nil {
		return nil, err
	}
	adjustment, err := positiveDecimal(ratio)
	if err != nil {
		return nil, err
	}
	if version.ScopeKind == PriceScopeChannel && !adjustment.Equal(decimal.NewFromInt(1)) {
		return nil, fmt.Errorf("channel price requires multiplier 1")
	}
	if err := billingexpr.ValidateAttributes(attributes); err != nil {
		return nil, err
	}
	if err := validatePriceWindow(version, at); err != nil {
		return nil, err
	}
	attrs := make(map[string]interface{}, len(attributes))
	for key, value := range attributes {
		attrs[key] = value
	}
	requestFacts, err := freezeRequestFacts(version.UsageSchema, facts)
	if err != nil {
		return nil, err
	}
	q := &Quote{ContractVersion: ContractVersion, CalculationVersion: CalculationVersion, PriceVersion: version, ChannelRatio: ratio, Attributes: attrs, FrozenAt: at.UTC(), RequestFacts: requestFacts}
	hash, err := Hash(q)
	if err != nil {
		return nil, err
	}
	q.Hash = hash
	return q, nil
}

func (u Usage) TokenParams(used map[string]bool) (billingexpr.TokenParams, error) {
	if u.Invalid {
		return billingexpr.TokenParams{}, fmt.Errorf("invalid provider billing usage")
	}
	values := []int64{u.InputTokens, u.OutputTokens, u.ContextTokens, u.CacheReadTokens, u.CacheWriteTokens, u.CacheWrite1hTokens, u.ImageInputTokens, u.ImageOutputTokens, u.AudioInputTokens, u.AudioOutputTokens, u.ReasoningTokens}
	for _, n := range values {
		if n < 0 || n > math.MaxInt32 {
			return billingexpr.TokenParams{}, fmt.Errorf("invalid billing token quantity")
		}
	}
	p, c := u.InputTokens, u.OutputTokens
	if u.ReasoningTokens > u.OutputTokens {
		return billingexpr.TokenParams{}, fmt.Errorf("invalid reasoning token quantity")
	}
	cr, img, imgCR := u.CacheReadTokens, u.ImageInputTokens, int64(0)
	if u.ImageCacheReadTokens != nil {
		n := *u.ImageCacheReadTokens
		if n < 0 || n > math.MaxInt32 || n > cr || n > img || (u.InputIncludesCache && (cr > p || img > p-(cr-n))) {
			return billingexpr.TokenParams{}, fmt.Errorf("invalid image cache token breakdown")
		}
		if used["img_cr"] && u.InputIncludesCache {
			imgCR = n
			cr -= n
			img -= n
		}
	}
	if u.InputIncludesCache {
		if used["cr"] {
			p -= cr
		}
		if used["cc"] {
			p -= u.CacheWriteTokens
		}
		if used["cc1h"] {
			p -= u.CacheWrite1hTokens
		}
	} else {
		if !used["cr"] {
			p += cr
		}
		if !used["cc"] {
			p += u.CacheWriteTokens
		}
		if !used["cc1h"] {
			p += u.CacheWrite1hTokens
		}
	}
	if used["img"] {
		p -= img
	}
	if used["img_cr"] {
		p -= imgCR
	}
	if used["ai"] {
		p -= u.AudioInputTokens
	}
	if used["img_o"] {
		c -= u.ImageOutputTokens
	}
	if used["ao"] {
		c -= u.AudioOutputTokens
	}
	if used["reasoning"] {
		c -= u.ReasoningTokens
	}
	if p < 0 {
		p = 0
	}
	if c < 0 {
		c = 0
	}
	return billingexpr.TokenParams{P: float64(p), C: float64(c), Len: float64(u.ContextTokens), CR: float64(cr), CC: float64(u.CacheWriteTokens), CC1h: float64(u.CacheWrite1hTokens), Img: float64(img), ImgCR: float64(imgCR), ImgO: float64(u.ImageOutputTokens), AI: float64(u.AudioInputTokens), AO: float64(u.AudioOutputTokens), Reasoning: float64(u.ReasoningTokens)}, nil
}

func Calculate(quote *Quote, usage Usage) (*ChargeResult, error) {
	if quote == nil {
		return nil, fmt.Errorf("missing frozen billing quote")
	}
	if quote.ContractVersion != ContractVersion || quote.CalculationVersion != CalculationVersion {
		return nil, fmt.Errorf("unsupported billing quote contract")
	}
	if err := ValidateFrozenPriceVersion(quote.PriceVersion); err != nil {
		return nil, err
	}
	if err := validatePriceWindow(quote.PriceVersion, quote.FrozenAt); err != nil {
		return nil, err
	}
	copyQuote := *quote
	copyQuote.Hash = ""
	hash, err := Hash(&copyQuote)
	if err != nil || hash != quote.Hash {
		return nil, fmt.Errorf("billing quote integrity mismatch")
	}
	ratio, err := positiveDecimal(quote.ChannelRatio)
	if err != nil {
		return nil, err
	}
	if quote.PriceVersion.ScopeKind == PriceScopeChannel && !ratio.Equal(decimal.NewFromInt(1)) {
		return nil, fmt.Errorf("channel price requires multiplier 1")
	}
	if quote.ChargeKind != "" {
		if quote.ChargeKind != "violation_fee" || !quote.ViolationFeeEnabled || !usage.Complete || usage.Estimated || usage.Invalid || usage.Source != "confirmed_violation" || len(usage.Quantities) != 1 || usage.Quantities["calls"] != "1" {
			return nil, fmt.Errorf("invalid frozen adjustment evidence")
		}
		params, err := usage.TokenParams(nil)
		if err != nil || params != (billingexpr.TokenParams{}) || len(usage.ActualFacts) != 0 || len(usage.EstimatedFacts) != 0 {
			return nil, fmt.Errorf("invalid frozen adjustment usage")
		}
		parent := *quote
		parent.Hash, parent.ChargeKind, parent.SourceQuoteHash = "", "", ""
		parentHash, err := Hash(parent)
		if err != nil || parentHash != quote.SourceQuoteHash {
			return nil, fmt.Errorf("source quote integrity mismatch")
		}
		fee, err := positiveDecimal(quote.ViolationFeeUSD)
		if err != nil {
			return nil, err
		}
		result := &ChargeResult{CalculationVersion: CalculationVersion, RoundingVersion: RoundingVersion,
			Lines: []ChargeLine{{Quantity: "violation_fee", Count: "1", Unit: "per_call", UnitPrice: fee.String(), SubtotalUSD: fee.String()}},
		}
		return finishCharge(result, fee, ratio)
	}
	if quote.SourceQuoteHash != "" {
		return nil, fmt.Errorf("unexpected source quote")
	}
	used := map[string]bool{}
	for _, price := range quote.PriceVersion.Prices {
		used[price.Quantity] = true
	}
	if quote.PriceVersion.Mode == ModeExpression {
		for key, value := range billingexpr.UsedVars(quote.PriceVersion.Expression) {
			used[key] = value
		}
	}
	params, err := usage.TokenParams(used)
	if err != nil {
		return nil, err
	}
	if quote.PriceVersion.UsageScope == UsageScopeEmbedding && (usage.OutputTokens > 0 || usage.ImageOutputTokens > 0 || usage.AudioOutputTokens > 0 || usage.ReasoningTokens > 0) {
		return nil, fmt.Errorf("embedding usage cannot contain output tokens")
	}
	tokenCounts := map[string]float64{"p": params.P, "c": params.C, "cr": params.CR, "cc": params.CC, "cc1h": params.CC1h, "img": params.Img, "img_cr": params.ImgCR, "img_o": params.ImgO, "ai": params.AI, "ao": params.AO, "reasoning": float64(usage.ReasoningTokens)}
	counts := map[string]decimal.Decimal{}
	facts, err := resolveUsageFacts(quote, usage)
	if err != nil {
		return nil, err
	}
	for key, value := range facts {
		if quote.PriceVersion.UsageSchema.Fields[key].Type == "number" {
			number, err := decimal.NewFromString(value.(string))
			if err != nil {
				return nil, err
			}
			counts[key] = number
		}
	}
	// Schema quantities are a separate namespace with explicit units. A field
	// named "p" or "c" must retain its fact value, not an unrelated token count.
	if quote.PriceVersion.UsageSchema == nil {
		for name, n := range tokenCounts {
			counts[name] = decimal.NewFromFloat(n)
		}
	}
	flatOnly := quote.PriceVersion.FlatFee != "" && len(quote.PriceVersion.Prices) == 0
	if quote.PriceVersion.UsageSchema == nil && quote.PriceVersion.Mode == ModeUnitPrice && !flatOnly && !used["calls"] && !used["images"] && !used["seconds"] {
		for _, name := range []string{"p", "c"} {
			if counts[name].IsPositive() && !used[name] {
				return nil, fmt.Errorf("missing price for quantity %s", name)
			}
		}
	}
	for name, quantity := range usage.Quantities {
		if quote.PriceVersion.UsageSchema != nil {
			return nil, fmt.Errorf("schema usage must use phase-specific facts")
		}
		n, err := positiveDecimal(quantity)
		if err != nil {
			return nil, fmt.Errorf("invalid usage quantity")
		}
		if tokenDimension(name) || n.GreaterThan(decimal.NewFromInt(math.MaxInt32)) {
			return nil, fmt.Errorf("invalid usage quantity")
		}
		if quote.PriceVersion.UsageScope == UsageScopeEmbedding && !n.IsZero() {
			return nil, fmt.Errorf("embedding usage cannot contain non-token quantities")
		}
		counts[name] = n
		if !n.IsZero() && !used[name] && !(name == "calls" && quote.PriceVersion.FlatFee != "") && quote.PriceVersion.Mode != ModeFree &&
			(quote.PriceVersion.Mode != ModeExpression || (name != "calls" && name != "images" && name != "seconds")) {
			return nil, fmt.Errorf("missing price for quantity %s", name)
		}
	}
	result := &ChargeResult{Lines: []ChargeLine{}, CalculationVersion: CalculationVersion, RoundingVersion: RoundingVersion}
	total := decimal.Zero
	if quote.PriceVersion.Mode == ModeExpression {
		input := billingexpr.RequestInput{Attributes: quote.Attributes, FrozenAt: quote.FrozenAt, Usage: facts, UsageSchema: quote.PriceVersion.UsageSchema}
		if quote.PriceVersion.UsesImageCount() {
			count, present := counts["images"]
			if !present || !count.IsInteger() || !count.IsPositive() || count.GreaterThan(decimal.NewFromInt(int64(dto.MaxImageN))) {
				return nil, fmt.Errorf("image_count requires a confirmed integer quantity between 1 and %d", dto.MaxImageN)
			}
			imageCount := int(count.IntPart())
			input.ImageCount = &imageCount
		}
		cost, trace, err := billingexpr.RunExprWithRequest(quote.PriceVersion.Expression, params, input)
		if err != nil {
			return nil, fmt.Errorf("billing expression evaluation failed")
		}
		total, err = finiteDecimalFromFloat(cost)
		if err != nil {
			return nil, err
		}
		if quote.PriceVersion.ResultUnit == ResultMicroUSD {
			total = total.Shift(-6)
		}
		result.Trace = &trace
		result.ExpressionVersion = billingexpr.ExprVersion(quote.PriceVersion.Expression)
		result.Lines = append(result.Lines, ChargeLine{Quantity: "expression", Count: "1", Unit: "per_call", UnitPrice: total.String(), SubtotalUSD: total.String()})
	}
	if quote.PriceVersion.Mode != ModeFree {
		prices := append([]UnitPrice(nil), quote.PriceVersion.Prices...)
		sort.Slice(prices, func(i, j int) bool { return prices[i].Quantity < prices[j].Quantity })
		for _, price := range prices {
			count := counts[price.Quantity]
			if count.IsZero() {
				continue
			}
			divisor := decimal.NewFromInt(1)
			if quote.PriceVersion.UsageSchema == nil {
				divisor, _ = unitDivisor(price.Unit)
			}
			if price.TierMode != "" {
				if err := addTieredCharge(result, &total, price, count, divisor); err != nil {
					return nil, err
				}
				continue
			}
			unitPrice, _ := positiveDecimal(price.Price)
			subtotal := count.Mul(unitPrice).DivRound(divisor, 48)
			total = total.Add(subtotal)
			result.Lines = append(result.Lines, ChargeLine{Quantity: price.Quantity, Count: count.String(), Unit: price.Unit, UnitPrice: unitPrice.String(), SubtotalUSD: subtotal.String()})
		}
	}
	if quote.PriceVersion.FlatFee != "" {
		flatFee, _ := positiveDecimal(quote.PriceVersion.FlatFee)
		if !flatFee.IsZero() {
			total = total.Add(flatFee)
			result.Lines = append(result.Lines, ChargeLine{Quantity: "flat_fee", Count: "1", Unit: "per_call", UnitPrice: flatFee.String(), SubtotalUSD: flatFee.String()})
		}
	}
	return finishCharge(result, total, ratio)
}

func finishCharge(result *ChargeResult, total, ratio decimal.Decimal) (*ChargeResult, error) {
	total = total.Mul(ratio)
	unrounded := total.Mul(decimal.NewFromInt(QuotaPerUSD))
	final, err := RoundCharge(unrounded)
	if err != nil {
		return nil, err
	}
	result.TotalUSD = total.String()
	result.UnroundedQuota = unrounded.String()
	result.FinalQuota = final
	result.RoundingDelta = decimal.NewFromInt(int64(final)).Sub(unrounded).String()
	return result, nil
}

func RoundCharge(quota decimal.Decimal) (common.Quota, error) {
	if quota.IsNegative() || quota.GreaterThan(decimal.NewFromInt(math.MaxInt32)) {
		return 0, fmt.Errorf("charge outside request limit")
	}
	rounded := quota.Round(0)
	if quota.IsPositive() && rounded.IsZero() {
		rounded = decimal.NewFromInt(1)
	}
	// The range check above proves this conversion fits both Quota and int32.
	return common.Quota(rounded.IntPart()), nil
}

func RequestQuota(quota common.Quota) (int, error) {
	if quota < 0 || quota > math.MaxInt32 {
		return 0, fmt.Errorf("charge outside request limit")
	}
	return int(quota), nil
}
