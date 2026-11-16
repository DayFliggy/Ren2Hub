package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// Every online top-up decides its bonus once per request. Epay, GMPay, Waffo
// and Waffo Pancake compute their payable amount locally and call quoteTopUp;
// Stripe and Creem charge remote prices and call quoteCreditedTopUpBonus.

// quoteTopUp prices one top-up of the native amount for the signed-in user,
// checks the wallet can take its full credit and writes the error response
// itself, so callers only return on nil.
func quoteTopUp(c *gin.Context, amount int64, pay decimal.Decimal, currency string) *service.TopUpQuote {
	principal, err := validateTopUpQuota(amount)
	if err != nil {
		paymentError(c, err)
		return nil
	}
	quote, err := service.QuoteTopUp(c.GetInt("id"), principal, pay, currency)
	if !acceptTopUpBonusQuote(c, err) {
		return nil
	}
	if !acceptTopUpCapacity(c, quote.TopUpBonusQuote) {
		return nil
	}
	return quote
}

// quoteCreditedTopUpBonus decides the bonus of a remotely priced top-up of
// quota, checks the wallet can take its full credit and writes the error
// response itself.
func quoteCreditedTopUpBonus(c *gin.Context, quota decimal.Decimal) *service.TopUpBonusQuote {
	principal, err := validateCreditedQuota(quota)
	if err != nil {
		paymentError(c, err)
		return nil
	}
	quote, err := service.QuoteTopUpBonus(c.GetInt("id"), principal)
	if !acceptTopUpBonusQuote(c, err) {
		return nil
	}
	if !acceptTopUpCapacity(c, quote) {
		return nil
	}
	return quote
}

func acceptTopUpBonusQuote(c *gin.Context, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, service.ErrTopUpBonusUnavailable):
		respondTopUpBonusUnavailable(c)
	default:
		paymentError(c, err)
	}
	return false
}

func acceptTopUpCapacity(c *gin.Context, quote *service.TopUpBonusQuote) bool {
	if err := model.ValidateTopUpQuotaCapacity(c.GetInt("id"), quote.ExpectedQuota()); err != nil {
		paymentError(c, err)
		return false
	}
	return true
}

// respondTopUpBonusUnavailable is the retryable answer when a live table
// cannot be read; the cause stays in the server log.
func respondTopUpBonusUnavailable(c *gin.Context) {
	respondTopUpPaymentCode(c, service.ErrTopUpBonusUnavailable, common.NewMessage("Top-up bonus is temporarily unavailable, please try again"), nil)
}

// respondTopUpPaymentCode answers in the payment {"message":"error"} shape
// with a stable code, the message key and optional extra fields.
func respondTopUpPaymentCode(c *gin.Context, code error, message *common.Message, extra gin.H) {
	body := gin.H{"message": "error", "code": code.Error(), "data": message.Error()}
	for key, value := range message.Fields() {
		body[key] = value
	}
	for key, value := range extra {
		body[key] = value
	}
	c.JSON(http.StatusOK, body)
}

// respondTopUpQuote answers an amount endpoint: data keeps the native payable
// string, quote carries the server's breakdown.
func respondTopUpQuote(c *gin.Context, quote *service.TopUpQuote) {
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": quote.PayAmount, "quote": quote.PublicView()})
}

// confirmTopUpQuote rejects a pay request whose confirmed price or bonus
// differs from the server's quote. It must run before any local order or
// gateway call.
func confirmTopUpQuote(c *gin.Context, expectation service.TopUpQuoteExpectation, quote *service.TopUpQuote) bool {
	switch err := expectation.Check(quote); {
	case err == nil:
		return true
	case errors.Is(err, service.ErrTopUpQuoteChanged):
		respondTopUpPaymentCode(c, err, common.NewMessage("The top-up price changed, please confirm the new price"), gin.H{"quote": quote.PublicView()})
	default:
		paymentError(c, common.NewMessage("Invalid parameters"))
	}
	return false
}

// confirmTopUpBonus is confirmTopUpQuote for remotely priced top-ups, whose
// confirmation carries only the bonus.
func confirmTopUpBonus(c *gin.Context, expectation service.TopUpBonusExpectation, quote *service.TopUpBonusQuote) bool {
	switch err := expectation.Check(quote); {
	case err == nil:
		return true
	case errors.Is(err, service.ErrTopUpBonusChanged):
		respondTopUpPaymentCode(c, err, common.NewMessage("The top-up bonus changed, please confirm the new bonus"), gin.H{"bonus": quote.PublicView()})
	default:
		paymentError(c, common.NewMessage("Invalid parameters"))
	}
	return false
}

// attachTopUpQuote freezes a locally priced quote on a new order.
func attachTopUpQuote(order *model.TopUp, quote *service.TopUpQuote) {
	attachTopUpBonus(order, quote.TopUpBonusQuote)
	order.Currency = quote.Currency
	order.PayAmount = quote.PayAmount
	order.Money = quote.PayDecimal().InexactFloat64()
}

// attachTopUpBonus freezes the full credit and its bonus on a new order and
// leaves the provider's own amount fields untouched.
func attachTopUpBonus(order *model.TopUp, quote *service.TopUpBonusQuote) {
	order.ExpectedQuota = quote.ExpectedQuota()
	order.BonusQuota = quote.BonusQuota()
	grant := quote.Grant
	order.Bonus = &grant
}

// GetTopUpBonusPreview answers the bonus of a credit of principal_quota, for
// top-ups whose price the provider decides.
func GetTopUpBonusPreview(c *gin.Context) {
	principal, err := strconv.Atoi(c.Query("principal_quota"))
	if err != nil || principal <= 0 || principal > common.MaxWalletQuota {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	quote, err := service.QuoteTopUpBonus(c.GetInt("id"), principal)
	if err != nil {
		respondSelfTopUpBonusError(c, err)
		return
	}
	common.ApiSuccess(c, quote.PublicView())
}
