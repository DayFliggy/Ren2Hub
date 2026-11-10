package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// The native Epay, GMPay, Waffo and Waffo Pancake handlers each compute their
// undiscounted payable amount, then call quoteTopUp exactly once per request.
// Stripe and Creem charge remote prices and are never discounted.

// quoteTopUp prices one top-up of the native amount for the signed-in user
// and writes the error response itself, so callers only return on nil.
func quoteTopUp(c *gin.Context, amount int64, list decimal.Decimal, currency string) *service.TopUpQuote {
	expectedQuota, err := validateTopUpQuota(amount)
	if err != nil {
		paymentError(c, err)
		return nil
	}
	quote, err := service.QuoteTopUp(c.GetInt("id"), expectedQuota, list, currency)
	if err != nil {
		if errors.Is(err, service.ErrTopUpDiscountUnavailable) {
			respondTopUpDiscountUnavailable(c)
			return nil
		}
		paymentError(c, err)
		return nil
	}
	return quote
}

// respondTopUpDiscountUnavailable is the retryable answer when a live table
// cannot be read; the cause stays in the server log.
func respondTopUpDiscountUnavailable(c *gin.Context) {
	message := common.NewMessage("Top-up discount is temporarily unavailable, please try again")
	body := gin.H{"message": "error", "code": service.ErrTopUpDiscountUnavailable.Error(), "data": message.Error()}
	for key, value := range message.Fields() {
		body[key] = value
	}
	c.JSON(http.StatusOK, body)
}

// respondTopUpQuote answers an amount endpoint: data keeps the native payable
// string, quote carries the server's breakdown.
func respondTopUpQuote(c *gin.Context, quote *service.TopUpQuote) {
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": quote.PayAmount, "quote": quote.PublicView()})
}

// confirmTopUpQuote rejects a pay request whose confirmed price differs from
// the server's quote. It must run before any local order or gateway call.
func confirmTopUpQuote(c *gin.Context, expectation service.TopUpQuoteExpectation, quote *service.TopUpQuote) bool {
	switch err := expectation.Check(quote); {
	case err == nil:
		return true
	case errors.Is(err, service.ErrTopUpQuoteChanged):
		body := gin.H{"message": "error", "code": err.Error(), "quote": quote.PublicView()}
		message := common.NewMessage("The top-up price changed, please confirm the new price")
		body["data"] = message.Error()
		for key, value := range message.Fields() {
			body[key] = value
		}
		c.JSON(http.StatusOK, body)
	default:
		paymentError(c, common.NewMessage("Invalid parameters"))
	}
	return false
}

// attachTopUpQuote freezes the quote on a new order.
func attachTopUpQuote(order *model.TopUp, quote *service.TopUpQuote) error {
	record, err := quote.Record()
	if err != nil {
		return err
	}
	order.ExpectedQuota = quote.ExpectedQuota
	order.Currency = quote.Currency
	order.PayAmount = quote.PayAmount
	order.Money = quote.PayDecimal().InexactFloat64()
	order.Discount = record
	return nil
}
