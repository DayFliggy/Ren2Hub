package controller

import (
	"context"
	"errors"
	"maps"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/QuantumNous/new-api/pkg/epay"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func GetTopUpInfo(c *gin.Context) {
	complianceConfirmed := operation_setting.IsPaymentComplianceConfirmed()

	// 获取支付方式
	payMethods := operation_setting.PayMethods
	if !complianceConfirmed {
		payMethods = []map[string]string{}
	}

	// 如果启用了 Stripe 支付，添加到支付方法列表
	if isStripeTopUpEnabled() {
		// 检查是否已经包含 Stripe
		hasStripe := false
		for _, method := range payMethods {
			if method["type"] == "stripe" {
				hasStripe = true
				break
			}
		}

		if !hasStripe {
			stripeMethod := map[string]string{
				"name":      "Stripe",
				"type":      "stripe",
				"color":     "#635BFF",
				"min_topup": strconv.Itoa(setting.StripeMinTopUp),
			}
			payMethods = append(payMethods, stripeMethod)
		}
	}

	// Waffo Pancake is displayed above the standard Waffo gateway.
	enableWaffoPancake := isWaffoPancakeTopUpEnabled()
	if enableWaffoPancake {
		hasWaffoPancake := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffoPancake {
				hasWaffoPancake = true
				break
			}
		}

		if !hasWaffoPancake {
			payMethods = append(payMethods, map[string]string{
				"name":      "Waffo Pancake",
				"type":      model.PaymentMethodWaffoPancake,
				"color":     "#F97316",
				"min_topup": strconv.Itoa(setting.WaffoPancakeMinTopUp),
			})
		}
	}

	// 如果启用了 Waffo 支付，添加到支付方法列表
	enableWaffo := isWaffoTopUpEnabled()
	if enableWaffo {
		hasWaffo := false
		for _, method := range payMethods {
			if method["type"] == model.PaymentMethodWaffo {
				hasWaffo = true
				break
			}
		}

		if !hasWaffo {
			waffoMethod := map[string]string{
				"name":      "Waffo (Global Payment)",
				"type":      model.PaymentMethodWaffo,
				"color":     "#3B82F6",
				"min_topup": strconv.Itoa(setting.WaffoMinTopUp),
			}
			payMethods = append(payMethods, waffoMethod)
		}
	}

	// Crypto assets appear beside the Epay methods. A broken GMPay configuration
	// reports itself unavailable and must never fail this response.
	enableGmpay := isGmpayTopUpEnabled()
	if enableGmpay {
		gmpayConfig, gmpayErr := service.LoadGmpayConfig(c.Request.Context())
		if gmpayErr != nil {
			enableGmpay = false
		} else if gmpayMethods, methodsErr := gmpayConfig.GmpayMethods(); methodsErr != nil {
			enableGmpay = false
		} else {
			for _, method := range gmpayMethods {
				hasGmpay := false
				for _, existing := range payMethods {
					if existing["type"] == method["type"] {
						hasGmpay = true
						break
					}
				}
				if hasGmpay {
					continue
				}
				payMethods = append(payMethods, map[string]string{
					"name": method["name"], "type": method["type"], "token": method["token"],
					"network": method["network"], "icon": method["icon"], "color": method["color"],
					"min_topup": method["min_topup"],
				})
			}
		}
	}

	data := gin.H{
		"enable_online_topup":              isEpayTopUpEnabled(),
		"enable_gmpay_topup":               enableGmpay,
		"enable_stripe_topup":              isStripeTopUpEnabled(),
		"enable_creem_topup":               isCreemTopUpEnabled(),
		"enable_waffo_topup":               enableWaffo,
		"enable_waffo_pancake_topup":       enableWaffoPancake,
		"enable_redemption":                complianceConfirmed,
		"payment_compliance_confirmed":     complianceConfirmed,
		"payment_compliance_terms_version": operation_setting.CurrentComplianceTermsVersion,
		"waffo_pay_methods": func() interface{} {
			if enableWaffo {
				return setting.GetWaffoPayMethods()
			}
			return nil
		}(),
		"creem_products":          setting.CreemProducts,
		"pay_methods":             payMethods,
		"min_topup":               operation_setting.MinTopUp,
		"stripe_min_topup":        setting.StripeMinTopUp,
		"waffo_min_topup":         setting.WaffoMinTopUp,
		"waffo_pancake_min_topup": setting.WaffoPancakeMinTopUp,
		"gmpay_min_topup":         setting.GmpayMinTopUp,
		"gmpay_currency":          "USD",
		"amount_options":          operation_setting.GetPaymentSetting().AmountOptions,
		"discount":                operation_setting.GetPaymentSetting().AmountDiscount,
		"topup_link":              common.TopUpLink,
	}
	common.ApiSuccess(c, data)
}

type EpayRequest struct {
	Amount           int64  `json:"amount"`
	PaymentMethod    string `json:"payment_method"`
	AmountUnit       string `json:"amount_unit"`
	QuoteFingerprint string `json:"quote_fingerprint"`
}

type AmountRequest struct {
	Amount int64 `json:"amount"`
}

// epayOrderError classifies a failed epay order creation so each HTTP
// contract (legacy React endpoint and /next Vue contract) can shape its own
// response while the order workflow stays single-sourced.
type epayOrderError struct {
	Code    string // PAYMENT_UNAVAILABLE / VALIDATION_ERROR / INTERNAL_ERROR
	Message *common.Message
}

type epayOrderResult struct {
	TradeNo   string
	Amount    int64 // stored amount, adjusted for the quota display type
	PayMoney  float64
	Method    string
	PayURL    string
	PayParams map[string]string
	Quote     *service.EpayQuote
}

// createEpayTopUpOrder is the single epay order workflow shared by the legacy
// endpoint (POST /api/user/pay) and the Vue contract
// (POST /api/next/wallet/topup). Validation, pricing, the upstream purchase
// call, and order persistence must not diverge between the two entry points;
// only request parsing and response shaping live in the callers.
func createEpayTopUpOrder(c *gin.Context, userID int, amount int64, method string, extra ...string) (*epayOrderResult, *epayOrderError) {
	request := service.EpayQuoteRequest{Amount: amount, PaymentMethod: method}
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
	result, err := service.CreateEpayTopUp(requestContext, userID, request)
	if err != nil {
		var business *service.EpayError
		if errors.As(err, &business) {
			return nil, &epayOrderError{Code: business.Code, Message: business.Message}
		}
		logger.LogError(requestContext, common.LogText("Epay failed to create order"))
		return nil, &epayOrderError{Code: "INTERNAL_ERROR", Message: common.NewMessage("Failed to create the payment order")}
	}
	logger.LogInfo(requestContext, common.LogText("Epay order created user_id=%d trade_no=%s", userID, result.Order.TradeNo))
	return &epayOrderResult{TradeNo: result.Order.TradeNo, Amount: result.Quote.Amount, PayMoney: result.Order.Money, Method: result.Quote.PaymentMethod, PayURL: result.URL, PayParams: result.Params, Quote: result.Quote}, nil
}

func getTopUpQuota(amount int64) (common.Quota, error) {
	quota := decimal.NewFromInt(amount)
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
		quota = decimal.NewFromInt(quota.Div(quotaPerUnit).IntPart()).Mul(quotaPerUnit)
	} else {
		quota = quota.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	return common.WalletQuotaFromDecimal(quota)
}

func getMaxTopUpAmount() int64 {
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	quotaPerUnit := decimal.NewFromFloat(common.QuotaPerUnit)
	maxStoredAmount := decimal.NewFromInt(math.MaxInt64).
		Div(quotaPerUnit).
		Floor()
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return math.MaxInt64
	}
	return maxStoredAmount.IntPart()
}

func validateCreditedQuota(quota decimal.Decimal) (common.Quota, error) {
	value, err := common.WalletQuotaFromDecimal(quota)
	if err != nil {
		return 0, common.NewMessage("Top-up quota exceeds the range the system can represent")
	}
	if value <= 0 {
		return 0, common.NewMessage("Top-up quota must be greater than 0")
	}
	return value, nil
}

func validateTopUpQuota(amount int64) (common.Quota, error) {
	quota, err := getTopUpQuota(amount)
	if err == nil && quota > 0 {
		return quota, nil
	}
	maxAmount := getMaxTopUpAmount()
	if maxAmount > 0 && amount > maxAmount {
		return 0, common.NewMessage("Top-up amount cannot exceed {{max}}", map[string]any{"max": maxAmount})
	}
	return 0, common.NewMessage("Invalid top-up amount")
}

// paymentError answers payment endpoints in their {"message": "error",
// "data": text} shape. When err is or wraps a *common.Message, the response
// also carries message_key and message_params for the web console.
func paymentError(c *gin.Context, err error) {
	body := gin.H{"message": "error", "data": err.Error()}
	var message *common.Message
	if errors.As(err, &message) {
		maps.Copy(body, message.Fields())
	}
	c.JSON(http.StatusOK, body)
}

func rejectInvalidCreditedQuota(c *gin.Context, userId int, quota decimal.Decimal) bool {
	creditedQuota, err := validateCreditedQuota(quota)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		paymentError(c, err)
		return true
	}
	return false
}

func rejectInvalidTopUpQuota(c *gin.Context, userId int, amount int64) bool {
	creditedQuota, err := validateTopUpQuota(amount)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(userId, creditedQuota)
	}
	if err != nil {
		paymentError(c, err)
		return true
	}
	return false
}

func RequestEpay(c *gin.Context) {
	var req EpayRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	order, orderErr := createEpayTopUpOrder(c, c.GetInt("id"), req.Amount, req.PaymentMethod, req.AmountUnit, req.QuoteFingerprint)
	if orderErr != nil {
		paymentError(c, orderErr.Message)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": order.PayParams, "url": order.PayURL})
}

// tradeNo lock
var orderLocks sync.Map
var createLock sync.Mutex

// refCountedMutex 带引用计数的互斥锁，确保最后一个使用者才从 map 中删除
type refCountedMutex struct {
	mu       sync.Mutex
	refCount int
}

// LockOrder 尝试对给定订单号加锁
func LockOrder(tradeNo string) {
	createLock.Lock()
	var rcm *refCountedMutex
	if v, ok := orderLocks.Load(tradeNo); ok {
		rcm = v.(*refCountedMutex)
	} else {
		rcm = &refCountedMutex{}
		orderLocks.Store(tradeNo, rcm)
	}
	rcm.refCount++
	createLock.Unlock()
	rcm.mu.Lock()
}

// UnlockOrder 释放给定订单号的锁
func UnlockOrder(tradeNo string) {
	v, ok := orderLocks.Load(tradeNo)
	if !ok {
		return
	}
	rcm := v.(*refCountedMutex)
	rcm.mu.Unlock()

	createLock.Lock()
	rcm.refCount--
	if rcm.refCount == 0 {
		orderLocks.Delete(tradeNo)
	}
	createLock.Unlock()
}

func EpayNotify(c *gin.Context) {
	params, err := epay.ParseNotification(c.Request)
	if err != nil {
		// Parameter values are never logged; a rejected genuine callback leaves
		// the order pending for administrator reconciliation.
		logger.LogWarn(c.Request.Context(), common.LogText("Epay callback parse rejected method=%s reason=%s", c.Request.Method, err.Error()))
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if strings.HasPrefix(params["out_trade_no"], "SUBUSR") {
		c.String(http.StatusOK, "fail")
		return
	}
	if err = service.SettleEpayNotification(c.Request.Context(), params, c.ClientIP()); err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("Epay callback rejected reason=%s", service.EpayFailureCode(err)))
		c.String(http.StatusOK, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

func RequestAmount(c *gin.Context) {
	var request service.EpayQuoteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	if request.PaymentMethod == "" {
		config, err := service.LoadEpayConfig(c.Request.Context())
		if err != nil || len(config.Methods) == 0 {
			paymentError(c, common.NewMessage("Payment method is unavailable"))
			return
		}
		request.PaymentMethod = config.Methods[0]["type"]
	}
	quote, _, err := service.QuoteEpay(c.Request.Context(), c.GetInt("id"), request)
	if err != nil {
		paymentError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "data": quote.PayAmount})
}

func GetUserTopUps(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchUserTopUps(userId, keyword, pageInfo)
	} else {
		topups, total, err = model.GetUserTopUps(userId, pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

// GetAllTopUps 管理员获取全平台充值记录
func GetAllTopUps(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	keyword := c.Query("keyword")

	var (
		topups []*model.TopUp
		total  int64
		err    error
	)
	if keyword != "" {
		topups, total, err = model.SearchAllTopUps(keyword, pageInfo)
	} else {
		topups, total, err = model.GetAllTopUps(pageInfo)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(topups)
	common.ApiSuccess(c, pageInfo)
}

type AdminCompleteTopupRequest struct {
	TradeNo string `json:"trade_no"`
}

// AdminCompleteTopUp 管理员补单接口
func AdminCompleteTopUp(c *gin.Context) {
	var req AdminCompleteTopupRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TradeNo == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}

	// 订单级互斥，防止并发补单
	LockOrder(req.TradeNo)
	defer UnlockOrder(req.TradeNo)

	if err := model.ManualCompleteTopUp(req.TradeNo, c.ClientIP()); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "user.topup_complete", map[string]interface{}{"trade_no": req.TradeNo})
	common.ApiSuccess(c, nil)
}
