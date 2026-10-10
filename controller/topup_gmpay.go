package controller

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

// GmpayNotify receives the gateway's signed JSON callback. The gateway expects
// HTTP 200 with "ok" or "success" to stop retrying, so a well-formed callback
// that cannot be settled still answers 200 with "fail" and leaves the order
// pending for the retry budget or for manual review.
func GmpayNotify(c *gin.Context) {
	params, err := gmpay.ParseNotification(c.Request)
	if err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("GMPay callback parse failed reason=%s", service.GmpayFailureCode(err)))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if err = service.SettleGmpayNotification(c.Request.Context(), params, c.ClientIP()); err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("GMPay callback rejected reason=%s", service.GmpayFailureCode(err)))
		c.String(http.StatusOK, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

// createGmpayTopUpOrder is the single GMPay order workflow shared by the wallet
// contract endpoints. It mirrors createEpayTopUpOrder: validation, pricing and
// persistence must not diverge between entry points, and a business rejection
// is translated into a stable code for the client.
func createGmpayTopUpOrder(c *gin.Context, userID int, amount int64, method string, extra ...string) (*service.GmpayCreatedOrder, *service.GmpayError) {
	request := service.GmpayQuoteRequest{Amount: amount, PaymentMethod: method}
	if len(extra) > 0 {
		request.AmountUnit = extra[0]
	}
	if len(extra) > 1 {
		request.QuoteFingerprint = extra[1]
	}
	requestContext := context.Background()
	if c.Request != nil {
		requestContext = c.Request.Context()
	}
	result, err := service.CreateGmpayTopUp(requestContext, userID, request)
	if err != nil {
		var business *service.GmpayError
		if errors.As(err, &business) {
			return nil, business
		}
		logger.LogError(requestContext, common.LogText("GMPay failed to create order"))
		return nil, &service.GmpayError{Code: "INTERNAL_ERROR", Message: common.NewMessage("Failed to create the payment order")}
	}
	logger.LogInfo(requestContext, common.LogText("GMPay order created user_id=%d trade_no=%s", userID, result.Order.TradeNo))
	return result, nil
}

// RequestGmpayAmount quotes the crypto rail. The payable amount the gateway
// signs is USD cents, so it is returned as text, never as a float.
func RequestGmpayAmount(c *gin.Context) {
	var request service.GmpayQuoteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	if strings.TrimSpace(request.PaymentMethod) == "" {
		config, err := service.LoadGmpayConfig(c.Request.Context())
		if err != nil || len(config.Assets) == 0 {
			paymentError(c, common.NewMessage("Payment method is unavailable"))
			return
		}
		request.PaymentMethod = service.GmpayMethodID(config.Assets[0])
	}
	quote, _, err := service.QuoteGmpay(c.Request.Context(), c.GetInt("id"), request)
	if err != nil {
		paymentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": quote.PayAmount})
}

type GmpayRequest struct {
	Amount           int64  `json:"amount"`
	AmountUnit       string `json:"amount_unit"`
	PaymentMethod    string `json:"payment_method"`
	QuoteFingerprint string `json:"quote_fingerprint"`
}

// RequestGmpay creates one crypto order and returns the hosted cashier address.
// The browser leaves this origin for that address, so it is validated before it
// is handed to the client.
func RequestGmpay(c *gin.Context) {
	var req GmpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	order, orderErr := createGmpayTopUpOrder(c, c.GetInt("id"), req.Amount, req.PaymentMethod, req.AmountUnit, req.QuoteFingerprint)
	if orderErr != nil {
		paymentError(c, orderErr)
		return
	}
	paymentURL, err := gmpay.ValidatePaymentURL(order.PaymentURL)
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("GMPay cashier URL is invalid trade_no=%s", order.Order.TradeNo))
		paymentError(c, common.NewMessage("Failed to create the payment order"))
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"url":     paymentURL,
		"data": gin.H{
			"trade_no":       order.Order.TradeNo,
			"payment_method": order.Quote.PaymentMethod,
			"currency":       order.Quote.Currency,
			"pay_amount":     order.Quote.PayAmount,
			"amount_minor":   strconv.FormatInt(order.Quote.AmountMinor, 10),
			"expected_quota": order.Quote.ExpectedQuota,
			"token":          order.Quote.Token,
			"network":        order.Quote.Network,
		},
	})
}

type saveGmpayRequest struct {
	PayAddress string  `json:"pay_address"`
	MerchantID string  `json:"merchant_id"`
	SecretKey  string  `json:"secret_key"`
	Assets     string  `json:"assets"`
	Price      float64 `json:"price"`
	MinTopUp   int     `json:"min_topup"`
	GroupRatio string  `json:"group_ratio"`
}

// ProbeGmpay fetches the gateway's public configuration so an administrator can
// pick concrete token.network pairs. It has no side effects and never creates an
// order. The public route is unauthenticated, so a success proves reachability
// and the asset catalog, not the merchant secret; the only route that checks the
// secret is order creation, which is never used as a probe.
func ProbeGmpay(c *gin.Context) {
	var req saveGmpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	address := strings.TrimSpace(req.PayAddress)
	if address == "" {
		common.ApiErrorT(c, "Enter the gateway address first")
		return
	}
	assets, err := service.GmpayGatewayIsReachable(c.Request.Context(), address)
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("GMPay gateway probe failed error=%s", err.Error()))
		common.ApiErrorT(c, "Cannot reach the gateway, or the gateway returned no available assets")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success", "data": gin.H{"supported_assets": assets}})
}

// SaveGmpay publishes the whole GMPay surface in one atomic option write. The
// gateway does not have to be online: an administrator may stage a
// configuration and enable it later.
func SaveGmpay(c *gin.Context) {
	var req saveGmpayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := service.SaveGmpayConfig(
		c.Request.Context(), req.PayAddress, req.MerchantID, req.SecretKey, req.Assets, req.Price, req.MinTopUp, req.GroupRatio,
	); err != nil {
		logger.LogError(c.Request.Context(), common.LogText("GMPay failed to save config error=%s", err.Error()))
		common.ApiErrorT(c, "Failed to save configuration")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "success", "data": gin.H{
		"pay_address": setting.GmpayPayAddress,
		"merchant_id": setting.GmpayId,
		"assets":      setting.GmpayAssets,
	}})
}

// NextGetGmpayTopUpConfig exposes the crypto rail beside the Epay rail. Only the
// rendered payment methods are returned: the merchant credential and the raw
// asset catalog must never reach a client. A broken GMPay configuration reports
// itself as unavailable and must never fail the Epay configuration request.
func NextGetGmpayTopUpConfig(c *gin.Context) {
	config, err := service.LoadGmpayConfig(c.Request.Context())
	if err != nil {
		common.ApiErrorT(c, "Failed to read the payment configuration")
		return
	}
	settlementEnabled, err := service.GmpaySettlementEnabled(c.Request.Context())
	if err != nil {
		common.ApiErrorT(c, "Failed to read the payment settlement configuration")
		return
	}
	methods := []map[string]string{}
	if config.Enabled() {
		if methods, err = config.GmpayMethods(); err != nil {
			common.ApiErrorT(c, "Invalid payment method configuration")
			return
		}
	}
	options := make([]int, 0, len(config.AmountOptions))
	for _, value := range config.AmountOptions {
		if value > 0 {
			options = append(options, value)
		}
	}
	common.ApiSuccess(c, gin.H{
		"enable_gmpay_topup":      config.Enabled(),
		"enable_gmpay_settlement": settlementEnabled,
		"pay_methods":             methods,
		"min_topup":               config.MinTopUp,
		"price":                   config.Price.String(),
		"amount_options":          options,
		"discount":                config.Discounts,
		// The snapshot records the requested network, not the final payment network.
		"crypto_callback_proves_network": false,
	})
}

func NextCreateGmpayTopUp(c *gin.Context) {
	var req nextTopUpRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	order, orderErr := createGmpayTopUpOrder(c, c.GetInt("id"), req.Amount, req.PaymentMethod, req.AmountUnit, req.QuoteFingerprint)
	if orderErr != nil {
		nextBusinessError(c, orderErr.Message, orderErr.Code)
		return
	}
	paymentURL, err := gmpay.ValidatePaymentURL(order.PaymentURL)
	if err != nil {
		logger.LogError(c.Request.Context(), common.LogText("GMPay cashier URL is invalid trade_no=%s", order.Order.TradeNo))
		nextBusinessError(c, common.NewMessage("Failed to create the payment order"), "INTERNAL_ERROR")
		return
	}
	common.ApiSuccess(c, gin.H{
		"trade_no":       order.Order.TradeNo,
		"status":         "PENDING",
		"amount":         order.Quote.Amount,
		"money":          order.Order.Money,
		"payment_method": order.Quote.PaymentMethod,
		"currency":       order.Quote.Currency,
		"pay_amount":     order.Quote.PayAmount,
		"token":          order.Quote.Token,
		"network":        order.Quote.Network,
		"quote":          order.Quote,
		"url":            paymentURL,
	})
}

// NextQuoteGmpayTopUp prices one crypto checkout and binds the requested asset
// to its confirmation fingerprint.
func NextQuoteGmpayTopUp(c *gin.Context) {
	var request service.GmpayQuoteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		nextBusinessError(c, common.NewMessage("Invalid parameters"), "VALIDATION_ERROR")
		return
	}
	quote, _, err := service.QuoteGmpay(c.Request.Context(), c.GetInt("id"), request)
	if err != nil {
		var business *service.GmpayError
		if errors.As(err, &business) {
			nextBusinessError(c, business.Message, business.Code)
			return
		}
		nextBusinessError(c, common.NewMessage("Payment operation failed. Please try again later."), "PAYMENT_ERROR")
		return
	}
	common.ApiSuccess(c, quote)
}
