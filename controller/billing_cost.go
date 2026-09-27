package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func RecordBillingUpstreamCost(c *gin.Context) {
	var command model.BillingCostCommand
	if err := common.DecodeJson(c.Request.Body, &command); err != nil {
		billingAPIError(c, model.ErrBillingCommandInvalid)
		return
	}
	cost, err := model.RecordBillingUpstreamCost(c.Request.Context(), c.Param("id"), c.GetInt("id"), command)
	if err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, cost)
}
