package types

import "github.com/shopspring/decimal"

// Usage discount decision states. Only HandleChannelRatio produces a decision;
// see docs/billing/USAGE_DISCOUNT.md.
const (
	UsageDiscountInactive      = "inactive"       // nothing configured; factor 1
	UsageDiscountOK            = "ok"             // computed from current windows
	UsageDiscountUnavailable   = "unavailable"    // usage data unreadable; fixed rate only
	UsageDiscountNotApplicable = "not_applicable" // channel tests; factor 1
)

// UsageDiscountDecision is the per-logical-request discount. It is computed
// once and reused by channel retries and later pricing of the same request.
type UsageDiscountDecision struct {
	Status        string            `json:"status"`
	Active        bool              `json:"active"` // some component is on or fixed_rate > 0
	FixedRate     decimal.Decimal   `json:"fixed_rate"`
	SiteRate      decimal.Decimal   `json:"site_rate"`
	DayRate       decimal.Decimal   `json:"day_rate"`
	WeekRate      decimal.Decimal   `json:"week_rate"`
	MonthRate     decimal.Decimal   `json:"month_rate"`
	RawRate       decimal.Decimal   `json:"raw_rate"`
	AppliedRate   decimal.Decimal   `json:"applied_rate"`
	CapRate       decimal.Decimal   `json:"cap_rate"`
	Factor        decimal.Decimal   `json:"factor"`
	WinnerWindow  string            `json:"winner_window,omitempty"`
	CapApplied    bool              `json:"cap_applied"`
	ShadowRates   map[string]string `json:"shadow_rates,omitempty"`
	ConfigVersion int64             `json:"config_version"`
	BusinessDate  string            `json:"business_date,omitempty"`
	Reason        string            `json:"reason,omitempty"`
}

// NeutralUsageDiscount returns a factor-1 decision with the given state.
func NeutralUsageDiscount(status string, reason string) *UsageDiscountDecision {
	return &UsageDiscountDecision{Status: status, Factor: decimal.NewFromInt(1), Reason: reason}
}
