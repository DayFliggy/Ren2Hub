package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	waffo "github.com/waffo-com/waffo-go"
	"github.com/waffo-com/waffo-go/core"
	"github.com/waffo-com/waffo-go/types/order"
)

// Notifications share the inquiry schema, but merchantInfo is optional. An
// inquiry with our credentials supplies the missing merchant ownership proof.
func verifyWaffoPaymentIdentity(ctx context.Context, sdk *waffo.Waffo, result *core.PaymentNotificationResult, originalAmount json.RawMessage) error {
	if len(originalAmount) > 0 || setting.WaffoMerchantId == "" || result.MerchantOrderID == "" {
		return model.ErrTopUpReceiptMismatch
	}
	if result.MerchantInfo != nil {
		if merchant, ok := result.MerchantInfo["merchantId"].(string); !ok || merchant != setting.WaffoMerchantId {
			return model.ErrTopUpReceiptMismatch
		}
		return nil
	}
	if result.PaymentRequestID == "" {
		return model.ErrTopUpReceiptMismatch
	}
	response, err := sdk.Order().Inquiry(ctx, &order.InquiryOrderParams{PaymentRequestID: result.PaymentRequestID}, nil)
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("Waffo inquiry returned no response")
	}
	for _, prefix := range []string{"[S0001]", "[S0004]", "[S0006]"} {
		if strings.HasPrefix(response.Message, prefix) {
			return fmt.Errorf("Waffo inquiry unavailable: %s", prefix)
		}
	}
	if response.Code == "A0009" {
		return model.ErrTopUpReceiptMismatch
	}
	if response.Code != "0" {
		return fmt.Errorf("Waffo inquiry unavailable: code=%s", response.Code)
	}
	data := response.GetData()
	if data == nil || data.PaymentRequestID != result.PaymentRequestID || data.MerchantOrderID != result.MerchantOrderID ||
		data.OrderStatus != "PAY_SUCCESS" || data.OrderCurrency != result.OrderCurrency ||
		!model.TopUpAmountsEqual(data.OrderAmount, result.OrderAmount) ||
		(data.MerchantInfo != nil && data.MerchantInfo.MerchantID != setting.WaffoMerchantId) {
		return model.ErrTopUpReceiptMismatch
	}
	return nil
}

func handleVerifiedWaffoPayment(c *gin.Context, sdk *waffo.Waffo, payload *webhookPayloadWithSubInfo) {
	wh := sdk.Webhook()
	if payload.Result.OrderStatus == "PAY_SUCCESS" {
		if err := verifyWaffoPaymentIdentity(c.Request.Context(), sdk, &payload.Result.PaymentNotificationResult, payload.Result.OriginalAmount); err != nil {
			if permanentPaymentReceiptError(err) {
				rejectPaymentReceipt(c.Request.Context(), model.PaymentProviderWaffo, payload.Result.MerchantOrderID, c.ClientIP(), "merchant_or_receipt_mismatch")
				sendWaffoWebhookResponse(c, wh, true, "")
			} else {
				logger.LogError(c.Request.Context(), common.LogText("Waffo merchant verification temporarily unavailable trade_no=%s error=%q", payload.Result.MerchantOrderID, err.Error()))
				// The provider retries failed acknowledgements; status also signals a retry
				// to HTTP intermediaries without changing the signed response contract.
				c.Status(http.StatusServiceUnavailable)
				sendWaffoWebhookResponse(c, wh, false, "merchant verification unavailable")
			}
			return
		}
	}
	handleWaffoPayment(c, wh, &payload.Result.PaymentNotificationResult)
}
