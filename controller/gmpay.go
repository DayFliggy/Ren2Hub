package controller

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func GmpayPay(c *gin.Context) {
	var request EpayRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		paymentError(c, common.NewMessage("Invalid parameters"))
		return
	}
	if request.Amount < getMinTopup() {
		paymentError(c, common.NewMessage("Top-up amount cannot be less than {{min}}", map[string]any{"min": getMinTopup()}))
		return
	}
	quota, err := validateTopUpQuota(request.Amount)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(c.GetInt("id"), quota)
	}
	if err != nil {
		paymentError(c, err)
		return
	}
	config, err := service.LoadGmpayConfig(c.Request.Context())
	if err != nil || !config.Enabled || !operation_setting.IsPaymentComplianceConfirmed() {
		paymentError(c, common.NewMessage("Payment method is unavailable"))
		return
	}
	money := getPayMoney(request.Amount)
	if math.IsNaN(money) || math.IsInf(money, 0) || money <= 0.01 {
		paymentError(c, common.NewMessage("Payment amount is too low"))
		return
	}
	amount := request.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	order, err := service.CreateGmpayTopUp(c.Request.Context(), service.GmpayOrderRequest{
		Order:         &model.TopUp{UserId: c.GetInt("id"), Amount: amount, Money: money, PaymentMethod: request.PaymentMethod},
		ExpectedQuota: quota, PayAmount: strconv.FormatFloat(money, 'f', 2, 64), ReturnURL: paymentReturnPath("/wallet"),
	}, config)
	if err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("GMPay order creation failed reason=%s", service.GmpayFailureCode(err)))
		gmpayCreationError(c, order)
		return
	}
	url, err := gmpay.ValidatePaymentURL(order.PaymentURL)
	if err != nil {
		paymentError(c, common.NewMessage("Failed to create the payment order"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "success", "url": url, "data": gin.H{"trade_no": order.Order.TradeNo, "payment_method": request.PaymentMethod, "currency": "CNY", "pay_amount": gmpay.FormatFiatAmount(order.Payment.AmountMinor), "expected_quota": quota, "token": order.Payment.Token, "network": order.Payment.Network}})
}

func gmpayCreationError(c *gin.Context, order *service.GmpayCreatedOrder) {
	message := common.NewMessage("Failed to create the payment order")
	body := gin.H{"message": "error", "data": message.Error()}
	for key, value := range message.Fields() {
		body[key] = value
	}
	if order != nil && order.Order != nil {
		body["trade_no"] = order.Order.TradeNo
		if order.Payment != nil {
			body["creation_status"] = order.Payment.Status
		}
	}
	c.JSON(http.StatusOK, body)
}

func GmpayNotify(c *gin.Context) {
	params, err := gmpay.ParseNotification(c.Request)
	if err != nil {
		c.String(http.StatusBadRequest, "fail")
		return
	}
	if err := service.SettleGmpayNotification(c.Request.Context(), params, c.ClientIP()); err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("GMPay callback rejected reason=%s", service.GmpayFailureCode(err)))
		c.String(http.StatusOK, "fail")
		return
	}
	c.String(http.StatusOK, "success")
}

func GetGmpayOrder(c *gin.Context) {
	// Authorize the native order before reading its private merchant version.
	var order model.TopUp
	err := model.DB.WithContext(c.Request.Context()).Where("trade_no = ? AND user_id = ? AND payment_provider = ?", c.Param("trade_no"), c.GetInt("id"), model.PaymentProviderGmpay).First(&order).Error
	if err != nil {
		common.ApiErrorT(c, "Top-up order not found")
		return
	}
	var payment model.GmpayOrder
	if err := model.DB.WithContext(c.Request.Context()).First(&payment, order.Id).Error; err != nil {
		common.ApiErrorT(c, "Failed to read the payment order")
		return
	}
	common.ApiSuccess(c, gin.H{"order": order, "payment": payment})
}

func GetGmpayConfig(c *gin.Context) {
	config, err := service.LoadGmpayConfig(c.Request.Context())
	if err != nil {
		common.ApiErrorT(c, "Failed to read the payment configuration")
		return
	}
	assets := config.Assets
	if assets == nil {
		assets = []setting.GmpayAsset{}
	}
	common.ApiSuccess(c, gin.H{"pay_address": config.PayAddress, "merchant_id": config.MerchantID, "has_secret": config.SecretKey != "", "assets": assets, "enabled": config.Enabled})
}

func SaveGmpayConfig(c *gin.Context) {
	var config setting.GmpayConfig
	if err := c.ShouldBindJSON(&config); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if err := service.SaveGmpayConfig(c.Request.Context(), config); err != nil {
		common.ApiErrorT(c, "Failed to save configuration")
		return
	}
	recordManageAudit(c, "payment.gmpay_config_update", map[string]any{"enabled": config.Enabled, "asset_count": len(config.Assets)})
	GetGmpayConfig(c)
}

func ProbeGmpay(c *gin.Context) {
	var request struct {
		PayAddress string `json:"pay_address"`
	}
	if err := c.ShouldBindJSON(&request); err != nil || strings.TrimSpace(request.PayAddress) == "" {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	assets, err := service.ProbeGmpayGateway(c.Request.Context(), request.PayAddress)
	if err != nil {
		common.ApiErrorT(c, "Cannot reach the gateway, or the gateway returned no available assets")
		return
	}
	common.ApiSuccess(c, gin.H{"supported_assets": assets})
}
