package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func billingAPIError(c *gin.Context, err error) {
	logger.LogError(c, "billing API error: "+err.Error())
	c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "billing operation unavailable"})
}

func billingPage(c *gin.Context) (int, int) {
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return offset, limit
}

func ListSelfBillingReservations(c *gin.Context) {
	offset, limit := billingPage(c)
	q := model.DB.WithContext(c.Request.Context()).Model(&model.BillingRecovery{}).Where("user_id = ? AND pre_consume_state = ? AND status NOT IN ?", c.GetInt("id"), model.BillingRecoveryPreConsumeCommitted, []string{model.BillingRecoveryStatusSettled, model.BillingRecoveryStatusRefunded})
	if tokenID := c.Query("token_id"); tokenID != "" {
		q = q.Where("token_id = ?", tokenID)
	}
	var total int64
	var rows []model.BillingRecovery
	if err := q.Count(&total).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	if err := q.Order("created_at desc, id desc").Offset(offset).Limit(limit).Find(&rows).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": billingRecoveryDTOs(rows), "total": total})
}

func ListSelfBillingEvents(c *gin.Context) {
	offset, limit := billingPage(c)
	q := model.DB.WithContext(c.Request.Context()).Model(&model.BillingEvent{}).Where("user_id = ?", c.GetInt("id"))
	if tokenID := c.Query("token_id"); tokenID != "" {
		q = q.Where("token_id = ?", tokenID)
	}
	var total int64
	var items []model.BillingEvent
	if err := q.Count(&total).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	if err := q.Order("id desc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": items, "total": total})
}

func billingRecoveryDTO(r model.BillingRecovery) gin.H {
	return gin.H{"request_id": r.RequestID, "user_id": r.UserID, "token_id": r.TokenID, "state": r.Status, "funding_amount": r.FundingAmount, "token_amount": r.TokenAmount, "version": r.Version, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt, "last_error": r.LastError}
}

func billingRecoveryDTOs(rows []model.BillingRecovery) []gin.H {
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		result = append(result, billingRecoveryDTO(row))
	}
	return result
}
