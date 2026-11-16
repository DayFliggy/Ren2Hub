package service

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// Top-up bonus decision states; see docs/billing/TOPUP_BONUS.md.
const (
	TopUpBonusInactive          = "inactive"           // nothing configured; no bonus
	TopUpBonusOK                = "ok"                 // every configured component was read
	TopUpBonusShadowUnavailable = "shadow_unavailable" // a shadow-only source failed; live components applied
)

var (
	// ErrTopUpBonusUnavailable means a table that grants a bonus could not be
	// read, so no credit may be decided without it.
	ErrTopUpBonusUnavailable = errors.New("topup_bonus_unavailable")
	// ErrTopUpQuoteChanged means the price or bonus the user confirmed is no
	// longer the server's quote; no order has been created.
	ErrTopUpQuoteChanged = errors.New("topup_quote_changed")
	// ErrTopUpBonusChanged means the bonus the user confirmed for a remotely
	// priced checkout is no longer the server's bonus; no order exists.
	ErrTopUpBonusChanged = errors.New("topup_bonus_changed")
	// ErrTopUpQuoteExpectationInvalid rejects a malformed confirmation.
	ErrTopUpQuoteExpectationInvalid = errors.New("topup_quote_expectation_invalid")
)

// TopUpBonusDecision is computed once per credit and frozen with it.
type TopUpBonusDecision struct {
	Status         string            `json:"status"`
	PrincipalQuota int64             `json:"principal_quota"`
	BonusQuota     int64             `json:"bonus_quota"`
	AmountRate     decimal.Decimal   `json:"amount_rate"`
	FixedRate      decimal.Decimal   `json:"fixed_rate"`
	SiteRate       decimal.Decimal   `json:"site_rate"`
	DayRate        decimal.Decimal   `json:"day_rate"`
	WeekRate       decimal.Decimal   `json:"week_rate"`
	MonthRate      decimal.Decimal   `json:"month_rate"`
	RawRate        decimal.Decimal   `json:"raw_rate"`
	AppliedRate    decimal.Decimal   `json:"applied_rate"`
	CapRate        decimal.Decimal   `json:"cap_rate"`
	WinnerWindow   string            `json:"winner_window,omitempty"`
	CapApplied     bool              `json:"cap_applied"`
	ShadowRates    map[string]string `json:"shadow_rates,omitempty"`
	ConfigVersion  int64             `json:"config_version"`
	BusinessDate   string            `json:"business_date,omitempty"`
	Reason         string            `json:"reason,omitempty"`
}

func neutralTopUpBonus(status string) *TopUpBonusDecision {
	return &TopUpBonusDecision{Status: status}
}

// TopUpBonusQuote is the bonus of one credit of PrincipalQuota, decided once
// per request; the preview, the stored order and its decision all use it.
type TopUpBonusQuote struct {
	PrincipalQuota int
	Decision       *TopUpBonusDecision
	Grant          model.TopUpBonusGrant
}

// BonusQuota is the bonus credited on top of the principal.
func (q *TopUpBonusQuote) BonusQuota() int { return q.Grant.BonusQuota }

// ExpectedQuota is the full credit: principal plus bonus.
func (q *TopUpBonusQuote) ExpectedQuota() int { return q.PrincipalQuota + q.Grant.BonusQuota }

// QuoteTopUpBonus decides the bonus of one credit of principal for userID.
func QuoteTopUpBonus(userID int, principal int) (*TopUpBonusQuote, error) {
	ctx, err := LoadTopUpBonusContext(userID, time.Now())
	if err != nil {
		return nil, err
	}
	decision := ctx.Evaluate(principal)
	grant, err := topUpBonusGrant(decision)
	if err != nil {
		return nil, err
	}
	return &TopUpBonusQuote{PrincipalQuota: principal, Decision: decision, Grant: grant}, nil
}

// PublicView is what the crediting user sees: no shadow rates or reasons.
func (q *TopUpBonusQuote) PublicView() map[string]any {
	d := q.Decision
	view := map[string]any{
		"principal_quota": q.PrincipalQuota,
		"bonus_quota":     q.BonusQuota(),
		"expected_quota":  q.ExpectedQuota(),
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

// TopUpQuote is the single price and bonus computation of one top-up request
// whose payable amount is computed locally (Epay, GMPay, Waffo and Pancake).
type TopUpQuote struct {
	*TopUpBonusQuote
	Currency  string
	PayAmount string
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

// QuoteTopUp prices one top-up of principal at the native payable amount pay
// in currency and decides its bonus. The bonus never changes the price.
func QuoteTopUp(userID int, principal int, pay decimal.Decimal, currency string) (*TopUpQuote, error) {
	bonus, err := QuoteTopUpBonus(userID, principal)
	if err != nil {
		return nil, err
	}
	currency = strings.ToUpper(currency)
	return &TopUpQuote{TopUpBonusQuote: bonus, Currency: currency, PayAmount: RoundTopUpPay(pay, currency)}, nil
}

// PayDecimal is the rounded payable amount.
func (q *TopUpQuote) PayDecimal() decimal.Decimal {
	return decimal.RequireFromString(q.PayAmount)
}

// PublicView adds the payable amount to the bonus view.
func (q *TopUpQuote) PublicView() map[string]any {
	view := q.TopUpBonusQuote.PublicView()
	view["currency"] = q.Currency
	view["pay_amount"] = q.PayAmount
	return view
}

var (
	topUpExpectedAmountPattern = regexp.MustCompile(`^[0-9]{1,24}(\.[0-9]{1,8})?$`)
	topUpExpectedQuotaPattern  = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})$`)
)

// TopUpBonusExpectation is the bonus the user confirmed, a JSON integer.
// Absent means a client that does not confirm bonuses; null is rejected.
type TopUpBonusExpectation struct {
	BonusQuota json.RawMessage `json:"expected_bonus_quota"`
}

// Check compares the confirmed bonus with the server's. It runs before any
// local order or gateway request exists.
func (e TopUpBonusExpectation) Check(quote *TopUpBonusQuote) error {
	if e.BonusQuota == nil {
		return nil
	}
	text := string(e.BonusQuota)
	if !topUpExpectedQuotaPattern.MatchString(text) {
		return ErrTopUpQuoteExpectationInvalid
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value > int64(common.MaxWalletQuota) {
		return ErrTopUpQuoteExpectationInvalid
	}
	if value != int64(quote.BonusQuota()) {
		return ErrTopUpBonusChanged
	}
	return nil
}

// TopUpQuoteExpectation is the price and bonus the user confirmed. The two
// price fields are both absent or both present; the bonus is independent.
type TopUpQuoteExpectation struct {
	PayAmount json.RawMessage `json:"expected_pay_amount"`
	Currency  json.RawMessage `json:"expected_currency"`
	TopUpBonusExpectation
}

// Check compares the confirmation with the server quote; any difference is
// ErrTopUpQuoteChanged, answered with the full quote.
func (e TopUpQuoteExpectation) Check(quote *TopUpQuote) error {
	if err := e.checkPrice(quote); err != nil {
		return err
	}
	if err := e.TopUpBonusExpectation.Check(quote.TopUpBonusQuote); err != nil {
		if errors.Is(err, ErrTopUpBonusChanged) {
			return ErrTopUpQuoteChanged
		}
		return err
	}
	return nil
}

func (e TopUpQuoteExpectation) checkPrice(quote *TopUpQuote) error {
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
