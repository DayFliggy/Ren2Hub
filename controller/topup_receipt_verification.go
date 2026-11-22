package controller

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/shopspring/decimal"
	"github.com/stripe/stripe-go/v81"
)

// The frozen checkout price is the subtotal, before promotion codes and tax.
// Adaptive Pricing reports that subtotal in the source currency separately.
func stripeVerifiedReceipt(event stripe.Event) (model.TopUpReceipt, error) {
	var session struct {
		AmountSubtotal *int64 `json:"amount_subtotal"`
		Currency       string `json:"currency"`
		Conversion     *struct {
			AmountSubtotal *int64 `json:"amount_subtotal"`
			SourceCurrency string `json:"source_currency"`
		} `json:"currency_conversion"`
	}
	if event.Data == nil {
		return model.TopUpReceipt{}, model.ErrTopUpReceiptMismatch
	}
	if err := common.Unmarshal(event.Data.Raw, &session); err != nil {
		return model.TopUpReceipt{}, model.ErrTopUpReceiptMismatch
	}
	amount, currency := session.AmountSubtotal, session.Currency
	if session.Conversion != nil {
		amount, currency = session.Conversion.AmountSubtotal, session.Conversion.SourceCurrency
	}
	if amount == nil || *amount <= 0 || currency == "" {
		return model.TopUpReceipt{}, model.ErrTopUpReceiptMismatch
	}
	return model.TopUpReceipt{Currency: strings.ToUpper(currency), Amount: stripePaymentAmount(*amount, currency)}, nil
}

// Only authenticated callbacks reach this audit path; do not persist raw payloads or credentials.
func rejectPaymentReceipt(ctx context.Context, provider, tradeNo, ip, reason string) {
	logger.LogWarn(ctx, common.LogText("Payment callback rejected provider=%s trade_no=%s reason=%s client_ip=%s", provider, tradeNo, reason, ip))
	model.RecordAuditLog(nil, model.AuditLog{
		Category: model.AuditCategorySecurity, Action: "payment.callback_rejected",
		AuthMethod: "webhook", Ip: ip, Status: http.StatusOK, Success: false,
		Content: "已验签支付回执未通过入账核验",
		Other:   model.AuditOther{RootInfo: model.AuditFields{"provider": provider, "trade_no": tradeNo, "reason": reason}},
	})
}

func permanentPaymentReceiptError(err error) bool {
	return errors.Is(err, model.ErrTopUpReceiptMismatch) || errors.Is(err, model.ErrTopUpNotFound) ||
		errors.Is(err, model.ErrPaymentMethodMismatch) || errors.Is(err, model.ErrTopUpStatusInvalid)
}

func pancakeVerifiedReceipt(event *service.WaffoPancakeWebhookEvent) (model.TopUpReceipt, error) {
	data := event.Data
	if setting.WaffoPancakeStoreID == "" || event.StoreID != setting.WaffoPancakeStoreID ||
		data.OrderStatus == nil || *data.OrderStatus != "completed" ||
		(data.PaymentStatus != nil && *data.PaymentStatus != "succeeded") {
		return model.TopUpReceipt{}, model.ErrTopUpReceiptMismatch
	}
	amount := data.Amount
	if data.ListPrice != nil {
		price := data.ListPrice
		if !pancakeListPriceHasNoDiscount(price) ||
			(price.ListTotal != nil && !model.TopUpAmountsEqual(*price.ListTotal, price.Total)) ||
			(data.ChargedAmount != nil && !model.TopUpAmountsEqual(*data.ChargedAmount, price.Total)) {
			return model.TopUpReceipt{}, model.ErrTopUpReceiptMismatch
		}
		amount = price.Subtotal
	} else if !model.TopUpAmountsEqual(data.TaxAmount, "0") {
		return model.TopUpReceipt{}, model.ErrTopUpReceiptMismatch
	}
	return model.TopUpReceipt{Currency: strings.ToUpper(data.Currency), Amount: amount}, nil
}

// Older payment records omit discountAmount. Their tax breakdown must still
// prove that the total contains no discount; missing tax is not assumed zero.
func pancakeListPriceHasNoDiscount(price *service.WaffoPancakeListPrice) bool {
	if price.DiscountAmount != nil {
		return model.TopUpAmountsEqual(*price.DiscountAmount, "0")
	}
	if price.TaxAmount == nil {
		return false
	}
	subtotal, err := decimal.NewFromString(price.Subtotal)
	if err != nil {
		return false
	}
	tax, err := decimal.NewFromString(*price.TaxAmount)
	return err == nil && model.TopUpAmountsEqual(subtotal.Add(tax).String(), price.Total)
}
