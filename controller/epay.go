package controller

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func writeEpayError(c *gin.Context, err error) {
	var business *service.EpayError
	if errors.As(err, &business) {
		nextBusinessError(c, business.Message, business.Code)
		return
	}
	if errors.Is(err, model.ErrEpayCheckThrottled) {
		nextBusinessError(c, "请稍后再检查支付结果", "RATE_LIMITED")
		return
	}
	if errors.Is(err, model.ErrEpaySettlementPaused) {
		nextBusinessError(c, "易支付结算已暂停", "SETTLEMENT_PAUSED")
		return
	}
	nextBusinessError(c, "支付操作失败，请稍后重试", "PAYMENT_ERROR")
}

func NextQuoteEpayTopUp(c *gin.Context) {
	var request service.EpayQuoteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		nextBusinessError(c, "参数错误", "VALIDATION_ERROR")
		return
	}
	quote, _, err := service.QuoteEpay(c.Request.Context(), c.GetInt("id"), request)
	if err != nil {
		writeEpayError(c, err)
		return
	}
	common.ApiSuccess(c, quote)
}

func NextVerifyEpayTopUp(c *gin.Context) {
	order := model.GetTopUpByTradeNo(c.Param("order_no"))
	if order == nil || order.UserId != c.GetInt("id") {
		nextBusinessError(c, "订单不存在", "NOT_FOUND")
		return
	}
	result, err := (service.EpayPaymentService{}).VerifyOrder(c.Request.Context(), order.TradeNo, c.GetInt("id"), c.ClientIP())
	if err != nil {
		writeEpayError(c, err)
		return
	}
	common.ApiSuccess(c, nextTopUpDTO(result, nil))
}

type epayRecoveryHandler struct{}

func (epayRecoveryHandler) Type() string            { return model.SystemTaskTypeEpayRecovery }
func (epayRecoveryHandler) Enabled() bool           { return true }
func (epayRecoveryHandler) Interval() time.Duration { return time.Minute }
func (epayRecoveryHandler) NewPayload() any         { return nil }
func (epayRecoveryHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	count, err := service.RecoverEpayOrders(ctx)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, map[string]int{"recovered": count}, err)
}
