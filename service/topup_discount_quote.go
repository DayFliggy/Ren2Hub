package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// Top-up discount decision states; see docs/billing/TOPUP_DISCOUNT.md.
const (
	TopUpDiscountInactive          = "inactive"           // nothing configured; factor 1
	TopUpDiscountOK                = "ok"                 // every configured component was read
	TopUpDiscountShadowUnavailable = "shadow_unavailable" // a shadow-only source failed; live components applied
)

var (
	// ErrTopUpDiscountUnavailable means a table that changes the price could
	// not be read, so no order may be priced without it.
	ErrTopUpDiscountUnavailable = errors.New("topup_discount_unavailable")
	// ErrTopUpQuoteChanged means the price the user confirmed is no longer
	// the server's price; no order has been created.
	ErrTopUpQuoteChanged = errors.New("topup_quote_changed")
	// ErrTopUpQuoteExpectationInvalid rejects a malformed confirmation.
	ErrTopUpQuoteExpectationInvalid = errors.New("topup_quote_expectation_invalid")
)

// TopUpDiscountDecision is computed once per top-up and frozen on the order.
type TopUpDiscountDecision struct {
	Status        string            `json:"status"`
	AmountRate    decimal.Decimal   `json:"amount_rate"`
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

func neutralTopUpDiscount(status string) *TopUpDiscountDecision {
	return &TopUpDiscountDecision{Status: status, Factor: decimal.NewFromInt(1)}
}

// TopUpQuote is the single price computation of one top-up request; the
// quote endpoint, the stored order and the gateway request all use it.
type TopUpQuote struct {
	ExpectedQuota int
	Currency      string
	ListPayAmount decimal.Decimal
	PayAmount     string
	Decision      *TopUpDiscountDecision
}

// zeroDecimalCurrencies are charged in whole units.
var zeroDecimalCurrencies = map[string]bool{"IDR": true, "JPY": true, "KRW": true, "VND": true}

func topUpCurrencyPlaces(currency string) int32 {
	if zeroDecimalCurrencies[currency] {
		return 0
	}
	return 2
}

// RoundTopUpPay is the only rounding of a payable amount: half-up at the
// currency's minor unit, as a fixed-point string.
func RoundTopUpPay(amount decimal.Decimal, currency string) string {
	return amount.StringFixed(topUpCurrencyPlaces(strings.ToUpper(currency)))
}

// QuoteTopUp prices one top-up of expectedQuota whose undiscounted payable
// amount is list in currency.
func QuoteTopUp(userID int, expectedQuota int, list decimal.Decimal, currency string) (*TopUpQuote, error) {
	settings, err := model.CurrentTopUpDiscountSettings()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTopUpDiscountUnavailable, err)
	}
	result, err := resolveTopUpDiscount(settings, userID, int64(expectedQuota), time.Now())
	if err != nil {
		return nil, err
	}
	currency = strings.ToUpper(currency)
	return &TopUpQuote{
		ExpectedQuota: expectedQuota,
		Currency:      currency,
		ListPayAmount: list,
		PayAmount:     RoundTopUpPay(list.Mul(result.Decision.Factor), currency),
		Decision:      result.Decision,
	}, nil
}

// PayDecimal is the rounded payable amount.
func (q *TopUpQuote) PayDecimal() decimal.Decimal {
	return decimal.RequireFromString(q.PayAmount)
}

// PublicView is what the paying user sees: no shadow rates or reasons.
func (q *TopUpQuote) PublicView() map[string]any {
	d := q.Decision
	view := map[string]any{
		"expected_quota":  q.ExpectedQuota,
		"currency":        q.Currency,
		"list_pay_amount": RoundTopUpPay(q.ListPayAmount, q.Currency),
		"pay_amount":      q.PayAmount,
		"factor":          d.Factor.String(),
		"applied_rate":    d.AppliedRate.String(),
		"amount_rate":     d.AmountRate.String(),
		"fixed_rate":      d.FixedRate.String(),
		"site_rate":       d.SiteRate.String(),
		"personal_rate":   decimal.Max(d.DayRate, d.WeekRate, d.MonthRate).String(),
		"cap_applied":     d.CapApplied,
	}
	if d.WinnerWindow != "" {
		view["winner_window"] = d.WinnerWindow
	}
	return view
}

// Record is the audit row frozen with the order, or nil when nothing was
// configured. It never takes part in callback settlement.
func (q *TopUpQuote) Record() (*model.TopUpDiscount, error) {
	if q.Decision.Status == TopUpDiscountInactive {
		return nil, nil
	}
	encoded, err := common.Marshal(q.Decision)
	if err != nil {
		return nil, err
	}
	return &model.TopUpDiscount{
		ConfigVersion: q.Decision.ConfigVersion,
		ListPayAmount: q.ListPayAmount.String(),
		Decision:      string(encoded),
	}, nil
}

var topUpExpectedAmountPattern = regexp.MustCompile(`^[0-9]{1,24}(\.[0-9]{1,8})?$`)

// TopUpQuoteExpectation is the price the user confirmed. Both fields absent
// means a client that does not confirm prices; both must be present otherwise.
type TopUpQuoteExpectation struct {
	PayAmount json.RawMessage `json:"expected_pay_amount"`
	Currency  json.RawMessage `json:"expected_currency"`
}

// Check compares the confirmation with the server quote. It runs before any
// local order or gateway request exists.
func (e TopUpQuoteExpectation) Check(quote *TopUpQuote) error {
	if e.PayAmount == nil && e.Currency == nil {
		return nil
	}
	var amount, currency string
	if common.Unmarshal(e.PayAmount, &amount) != nil || common.Unmarshal(e.Currency, &currency) != nil {
		return ErrTopUpQuoteExpectationInvalid
	}
	if !topUpExpectedAmountPattern.MatchString(amount) || currency == "" || len(currency) > 3 {
		return ErrTopUpQuoteExpectationInvalid
	}
	currency = strings.ToUpper(currency)
	expected := decimal.RequireFromString(amount)
	// Trailing zeros are fine; digits below the currency's minor unit are not.
	if !expected.Equal(expected.Round(topUpCurrencyPlaces(currency))) {
		return ErrTopUpQuoteExpectationInvalid
	}
	if currency != quote.Currency || !expected.Equal(quote.PayDecimal()) {
		return ErrTopUpQuoteChanged
	}
	return nil
}
