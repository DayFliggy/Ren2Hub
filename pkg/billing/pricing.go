// Package billing owns the reproducible pricing contract. It has no database,
// current configuration or provider dependencies. Evaluation captures and
// authenticates live environment reads before they can be replayed.
package billing

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"math"

	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"
)

const (
	ModeRatio          = "ratio"
	ModePerCall        = "per_call"
	ModeExpression     = "expression"
	CalculationVersion = "newapi-c71eefbc-v1"
	ContractVersion    = 4
	ResultMicroUSD     = "micro_usd"
	ResultUSD          = "usd"
	RoundingVersion    = "half-up-positive-minimum-v1"
	QuotaPerUSD        = int64(500000)
	PriceScopeGlobal   = "global_model"
)

// UnitPrice is USD per declared unit. Quantity is a named usage dimension;
// quantities never come from arbitrary request paths.
type UnitPrice struct {
	Quantity string `json:"quantity"`
	Unit     string `json:"unit"`
	Price    string `json:"price"`
}

type PriceVersion struct {
	Pricing            PricingValues            `json:"pricing,omitempty"`
	TaskUsage          bool                     `json:"task_usage,omitempty"`
	ContractVersion    int                      `json:"contract_version"`
	CalculationVersion string                   `json:"calculation_version"`
	ResultUnit         string                   `json:"result_unit"`
	UsageSchema        *billingexpr.UsageSchema `json:"usage_schema,omitempty"`
	ID                 int64                    `json:"id"`
	ScopeKind          string                   `json:"scope_kind"`
	ChannelID          int                      `json:"channel_id,omitempty"`
	Model              string                   `json:"model"`
	Mode               string                   `json:"mode"`
	Prices             []UnitPrice              `json:"prices"`
	Expression         string                   `json:"expression,omitempty"`
	CreatedAt          int64                    `json:"created_at"`
	Hash               string                   `json:"hash"`
}

// NeedsTokenUsage determines whether this model's price consumes token counts.
func (version PriceVersion) NeedsTokenUsage() bool {
	if version.TaskUsage {
		return false
	}
	if version.Mode == ModeRatio {
		return true
	}
	if version.Mode == ModeExpression {
		for name := range billingexpr.UsedVars(version.Expression) {
			if name == "len" || tokenDimension(name) {
				return true
			}
		}
	}
	for _, price := range version.Prices {
		if tokenDimension(price.Quantity) {
			return true
		}
	}
	return false
}

func (version PriceVersion) UsesImageCount() bool {
	expressionVersion, _ := billingexpr.ParseExprVersion(version.Expression)
	return version.Mode == ModeExpression && expressionVersion == 1 && billingexpr.UsedVars(version.Expression)["image_count"]
}

type Quote struct {
	PreConsumeMultiplier string                 `json:"pre_consume_multiplier,omitempty"`
	OtherRatios          map[string]string      `json:"other_ratios,omitempty"`
	AudioInputPrice      string                 `json:"audio_input_price,omitempty"`
	ContractVersion      int                    `json:"contract_version"`
	CalculationVersion   string                 `json:"calculation_version"`
	PriceVersion         PriceVersion           `json:"price_version"`
	ChannelRatio         string                 `json:"channel_ratio"`
	Attributes           map[string]interface{} `json:"attributes,omitempty"`
	FrozenAt             time.Time              `json:"frozen_at"`
	Hash                 string                 `json:"hash"`
	ChargeKind           string                 `json:"charge_kind,omitempty"`
	SourceQuoteHash      string                 `json:"source_quote_hash,omitempty"`
	ViolationFeeEnabled  bool                   `json:"violation_fee_enabled"`
	ViolationFeeUSD      string                 `json:"violation_fee_usd"`
}

// Quantities hold decimal counts for calls/images/seconds/tool:<name>. Tokens
// retain the canonical input semantics independent from protocol projections.
type Usage struct {
	PricingKind          string               `json:"pricing_kind,omitempty"`
	Evidence             *EncryptedEvaluation `json:"evaluation_evidence,omitempty"`
	requestInput         *billingexpr.RequestInput
	environmentCalls     []billingexpr.EnvironmentCall
	ActualServiceTier    string                 `json:"actual_service_tier,omitempty"`
	Terminal             string                 `json:"terminal,omitempty"`
	ObservationIDs       []string               `json:"observation_ids,omitempty"`
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
	AudioCacheReadTokens *int64                 `json:"audio_cache_read_tokens,omitempty"`
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

func (usage Usage) ReadyForSettlement() bool {
	return usage.Complete && !usage.Estimated && !usage.Invalid &&
		strings.TrimSpace(usage.Source) != "" && usage.Terminal == "completed"
}

type ChargeLine struct {
	Quantity    string `json:"quantity"`
	Count       string `json:"count"`
	Unit        string `json:"unit"`
	UnitPrice   string `json:"unit_price"`
	SubtotalUSD string `json:"subtotal_usd"`
}

type ChargeAdjustment struct {
	Kind       string `json:"kind"`
	Multiplier string `json:"multiplier"`
	BaseUSD    string `json:"base_usd"`
	AmountUSD  string `json:"amount_usd"`
	ResultUSD  string `json:"result_usd"`
}

type ChargeResult struct {
	BaseUSD            string                   `json:"base_usd"`
	Adjustments        []ChargeAdjustment       `json:"adjustments"`
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

func tokenDimension(name string) bool {
	switch name {
	case "p", "c", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ai_cr", "ao", "reasoning":
		return true
	}
	return false
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
	if version.ContractVersion == 0 && version.ID == 0 && version.Hash == "" {
		version.ContractVersion = ContractVersion
	}
	if version.CalculationVersion == "" && version.ID == 0 && version.Hash == "" {
		version.CalculationVersion = CalculationVersion
	}
	if version.ScopeKind == "" {
		version.ScopeKind = PriceScopeGlobal
	}
	if version.ResultUnit == "" {
		version.ResultUnit = ResultUSD
		if version.Mode == ModeExpression && !version.TaskUsage {
			version.ResultUnit = ResultMicroUSD
		}
	}
	if err := ValidateFrozenPriceVersion(version); err != nil {
		return version, err
	}
	encoded, err := common.Marshal(version)
	if err != nil {
		return version, err
	}
	var frozen PriceVersion
	if err := common.DecodeJsonNumbers(bytes.NewReader(encoded), &frozen); err != nil {
		return version, err
	}
	if frozen.Hash == "" {
		definition := frozen
		definition.ID, definition.CreatedAt, definition.Hash = 0, 0, ""
		frozen.Hash, err = Hash(definition)
		if err != nil {
			return version, err
		}
	}
	return frozen, nil
}
func ValidateFrozenPriceVersion(version PriceVersion) error {
	if version.Pricing == nil {
		return fmt.Errorf("model pricing configuration is required")
	}
	return validateModelPriceVersion(version)
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
	copyQuote.ChargeKind, copyQuote.SourceQuoteHash = "violation_fee", original.Hash
	copyQuote.Hash, err = Hash(copyQuote)
	if err != nil {
		return nil, err
	}
	return &copyQuote, nil
}

func NewQuote(version PriceVersion, ratio string, attributes map[string]interface{}, at time.Time) (*Quote, error) {
	version, err := PreparePriceVersion(version)
	if err != nil {
		return nil, err
	}
	if _, err := positiveDecimal(ratio); err != nil {
		return nil, err
	}
	if at.IsZero() {
		return nil, fmt.Errorf("billing quote time is required")
	}
	attrs, err := billingexpr.FreezeAttributes(attributes)
	if err != nil {
		return nil, err
	}
	quote := &Quote{ContractVersion: ContractVersion, CalculationVersion: CalculationVersion, PriceVersion: version, ChannelRatio: ratio, Attributes: attrs, FrozenAt: at.UTC()}
	quote.Hash, err = Hash(quote)
	return quote, err
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
	inputTotal := u.InputTokens
	if !u.InputIncludesCache {
		inputTotal += u.CacheReadTokens + u.CacheWriteTokens + u.CacheWrite1hTokens
	}
	// Image and audio buckets are disjoint modalities. Cache buckets may
	// overlap a modality, but cannot explain modality totals above all input.
	if u.ImageInputTokens+u.AudioInputTokens > inputTotal ||
		u.ImageOutputTokens+u.AudioOutputTokens+u.ReasoningTokens > u.OutputTokens {
		return billingexpr.TokenParams{}, fmt.Errorf("media token breakdown exceeds aggregate usage")
	}
	cr, img, ai := u.CacheReadTokens, u.ImageInputTokens, u.AudioInputTokens
	imgCR, aiCR := int64(0), int64(0)
	var cacheDetails int64
	for _, detail := range []struct {
		value            *int64
		total            int64
		key              string
		cache, remainder *int64
	}{
		{u.ImageCacheReadTokens, img, "img_cr", &imgCR, &img},
		{u.AudioCacheReadTokens, ai, "ai_cr", &aiCR, &ai},
	} {
		if detail.value == nil {
			continue
		}
		n := *detail.value
		if n < 0 || n > math.MaxInt32 || n > u.CacheReadTokens || n > detail.total || (u.InputIncludesCache && (u.CacheReadTokens > p || detail.total > p-(u.CacheReadTokens-n))) {
			return billingexpr.TokenParams{}, fmt.Errorf("invalid %s cache token breakdown", map[string]string{"img_cr": "image", "ai_cr": "audio"}[detail.key])
		}
		cacheDetails += n
		if used[detail.key] && u.InputIncludesCache {
			*detail.cache = n
			cr -= n
			*detail.remainder -= n
		}
	}
	if cacheDetails > u.CacheReadTokens {
		return billingexpr.TokenParams{}, fmt.Errorf("media cache details exceed aggregate cache")
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
	}
	if u.InputIncludesCache && used["img"] {
		p -= img
	}
	if u.InputIncludesCache && used["img_cr"] {
		p -= imgCR
	}
	if u.InputIncludesCache && used["ai"] {
		p -= ai
	}
	if u.InputIncludesCache && used["ai_cr"] {
		p -= aiCR
	}
	if u.InputIncludesCache && used["img_o"] {
		c -= u.ImageOutputTokens
	}
	if u.InputIncludesCache && used["ao"] {
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
	return billingexpr.TokenParams{P: p, C: c, Len: inputTotal, CR: cr, CC: u.CacheWriteTokens, CC1h: u.CacheWrite1hTokens, Img: img, ImgCR: imgCR, ImgO: u.ImageOutputTokens, AI: ai, AICR: aiCR, AO: u.AudioOutputTokens, Reasoning: u.ReasoningTokens}, nil
}

func Calculate(quote *Quote, usage Usage) (*ChargeResult, error) {
	result, err := calculate(quote, usage)
	RecordCalculation(err == nil)
	return result, err
}

// ValidateQuote checks frozen identity and policy without requiring usage. It
// is used before reservation as well as by calculation and audit replay.
func ValidateQuote(quote *Quote) error {
	if quote == nil {
		return fmt.Errorf("missing frozen billing quote")
	}
	if quote.ContractVersion != ContractVersion || quote.CalculationVersion != CalculationVersion {
		return fmt.Errorf("unsupported billing quote contract")
	}
	if err := ValidateFrozenPriceVersion(quote.PriceVersion); err != nil {
		return err
	}
	if quote.FrozenAt.IsZero() {
		return fmt.Errorf("billing quote time is required")
	}
	copyQuote := *quote
	copyQuote.Hash = ""
	hash, err := Hash(&copyQuote)
	if err != nil || hash != quote.Hash {
		return fmt.Errorf("billing quote integrity mismatch")
	}
	if _, err := positiveDecimal(quote.ChannelRatio); err != nil {
		return err
	}
	if quote.PreConsumeMultiplier != "" {
		factor, err := positiveDecimal(quote.PreConsumeMultiplier)
		if err != nil || !factor.IsPositive() {
			return fmt.Errorf("invalid pre-consume multiplier")
		}
	}
	for _, value := range quote.OtherRatios {
		factor, err := positiveDecimal(value)
		if err != nil || !factor.IsPositive() {
			return fmt.Errorf("invalid request price multiplier")
		}
	}
	if quote.AudioInputPrice != "" {
		if _, err := positiveDecimal(quote.AudioInputPrice); err != nil {
			return fmt.Errorf("invalid audio input price")
		}
	}
	return nil
}

func calculate(quote *Quote, usage Usage) (*ChargeResult, error) {
	if err := ValidateQuote(quote); err != nil {
		return nil, err
	}
	if usage.Evidence != nil && usage.requestInput == nil {
		return nil, fmt.Errorf("encrypted billing evidence must be replayed with its cipher")
	}
	ratio, _ := positiveDecimal(quote.ChannelRatio)
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
	return calculateModelPricing(quote, usage)
}

func finishCharge(result *ChargeResult, total, ratio decimal.Decimal) (*ChargeResult, error) {
	result.BaseUSD = total.String()
	adjusted := total.Mul(ratio)
	result.Adjustments = []ChargeAdjustment{{Kind: "channel_multiplier", Multiplier: ratio.String(), BaseUSD: total.String(), AmountUSD: adjusted.Sub(total).String(), ResultUSD: adjusted.String()}}
	total = adjusted
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

// UnmarshalJSON preserves exact request attributes and usage facts during recovery.
func (p *PriceVersion) UnmarshalJSON(data []byte) error {
	type wire PriceVersion
	var value wire
	if err := common.DecodeJsonNumbers(bytes.NewReader(data), &value); err != nil {
		return err
	}
	*p = PriceVersion(value)
	return nil
}

func (q *Quote) UnmarshalJSON(data []byte) error {
	type wire Quote
	var value wire
	if err := common.DecodeJsonNumbers(bytes.NewReader(data), &value); err != nil {
		return err
	}
	*q = Quote(value)
	return nil
}
func (u *Usage) UnmarshalJSON(data []byte) error {
	type wire Usage
	var value wire
	if err := common.DecodeJsonNumbers(bytes.NewReader(data), &value); err != nil {
		return err
	}
	*u = Usage(value)
	return nil
}
