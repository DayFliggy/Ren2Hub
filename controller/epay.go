package controller

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func writeEpayError(c *gin.Context, err error) {
	var business *service.EpayError
	if errors.As(err, &business) {
		nextBusinessError(c, business.Message, business.Code)
		return
	}
	nextBusinessError(c, common.NewMessage("Payment operation failed. Please try again later."), "PAYMENT_ERROR")
}

func NextQuoteEpayTopUp(c *gin.Context) {
	var request service.EpayQuoteRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		nextBusinessError(c, common.NewMessage("Invalid parameters"), "VALIDATION_ERROR")
		return
	}
	quote, _, err := service.QuoteEpay(c.Request.Context(), c.GetInt("id"), request)
	if err != nil {
		writeEpayError(c, err)
		return
	}
	common.ApiSuccess(c, quote)
}
