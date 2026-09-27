package types

import (
	"fmt"
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/pkg/billing"
)

type PriceData struct {
	Quote             *billing.Quote        `json:"quote,omitempty"`
	Usage             *billing.Usage        `json:"usage,omitempty"`
	Charge            *billing.ChargeResult `json:"charge,omitempty"`
	FreeModel         bool
	UsePrice          bool
	Quota             int // 按次计费的最终额度（MJ / Task）
	QuotaToPreConsume int // 按量计费的预消耗额度
}

// ChannelMultiplier is a diagnostic projection of the immutable quote. It is
// not mutable request state and cannot be used as a settlement input.
func (p *PriceData) ChannelMultiplier() float64 {
	if p == nil || p.Quote == nil || p.Quote.ChannelRatio == "" {
		return 1
	}
	ratio, err := strconv.ParseFloat(p.Quote.ChannelRatio, 64)
	if err != nil || ratio <= 0 || math.IsNaN(ratio) || math.IsInf(ratio, 0) {
		return 1
	}
	return ratio
}

func (p *PriceData) ToSetting() string {
	if p == nil {
		return "missing price data"
	}
	if p.Quote == nil {
		return fmt.Sprintf("UsePrice: %t, QuotaToPreConsume: %d, Quota: %d", p.UsePrice, p.QuotaToPreConsume, p.Quota)
	}
	return fmt.Sprintf("Scope: %s, ChannelID: %d, PriceVersionID: %d, QuoteHash: %s, Multiplier: %s, UsePrice: %t, QuotaToPreConsume: %d, Quota: %d", p.Quote.PriceVersion.ScopeKind, p.Quote.PriceVersion.ChannelID, p.Quote.PriceVersion.ID, p.Quote.Hash, p.Quote.ChannelRatio, p.UsePrice, p.QuotaToPreConsume, p.Quota)
}
