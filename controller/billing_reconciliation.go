package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func billingAPIError(c *gin.Context, err error) {
	status := http.StatusInternalServerError
	message := "billing operation unavailable"
	if errors.Is(err, model.ErrBillingCommandInvalid) {
		status = http.StatusBadRequest
	}
	if errors.Is(err, model.ErrBillingEventConflict) || errors.Is(err, model.ErrBillingVersionConflict) || errors.Is(err, model.ErrBillingRecoveryStateConflict) {
		status = http.StatusConflict
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = http.StatusNotFound
	}
	if status != http.StatusInternalServerError {
		message = err.Error()
	} else {
		logger.LogError(c, "billing API error: "+err.Error())
	}
	c.JSON(status, gin.H{"success": false, "message": message})
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

func ListBillingReconciliations(c *gin.Context) {
	q := model.DB.WithContext(c.Request.Context()).Model(&model.BillingRecovery{})
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	} else {
		q = q.Where("status IN ? OR consumption_state IN ? OR (status IN ? AND consumption_state <> ?)", []string{model.BillingRecoveryStatusReconciliation, model.BillingRecoveryStatusSettlementPending, model.BillingRecoveryStatusRefundPending}, []string{model.BillingRecoveryStateUsagePending, model.BillingRecoveryStateUsageCollecting, model.BillingRecoveryStatePricingPending, model.BillingRecoveryStatePricingCollecting, model.BillingRecoveryStateUnprepared}, []string{model.BillingRecoveryStatusSettled, model.BillingRecoveryStatusRefunded}, model.BillingRecoveryStateCompleted)
	}
	var total int64
	var items []model.BillingRecovery
	offset, limit := billingPage(c)
	if err := q.Count(&total).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	if err := q.Order("updated_at asc, id asc").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"items": billingRecoveryDTOs(items), "total": total})
}

func GetBillingReconciliation(c *gin.Context) {
	var row model.BillingRecovery
	if err := model.DB.Where("request_id = ?", c.Param("id")).First(&row).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	id := row.ID
	var recovery model.BillingRecovery
	if err := model.DB.WithContext(c.Request.Context()).First(&recovery, id).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	var events []model.BillingEvent
	if err := model.DB.WithContext(c.Request.Context()).Where("request_id = ?", recovery.RequestID).Order("id asc").Find(&events).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	decode := func(raw string) any {
		if raw == "" {
			return nil
		}
		var value any
		if err := common.UnmarshalJsonStr(raw, &value); err != nil {
			return gin.H{"unavailable": true}
		}
		return value
	}
	var attempts []model.BillingAttempt
	if err := model.DB.WithContext(c.Request.Context()).Where("request_id = ?", recovery.RequestID).Order("created_at asc, id asc").Find(&attempts).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	quotes := make([]gin.H, 0, len(attempts))
	for _, attempt := range attempts {
		quotes = append(quotes, gin.H{"id": attempt.ID, "channel_id": attempt.ChannelID, "request_fingerprint": attempt.Fingerprint, "quote_hash": attempt.QuoteHash, "created_at": attempt.CreatedAt, "quote": decode(attempt.Quote)})
	}
	audit := model.AuditBillingEvidence(recovery, attempts, events)
	costs, err := model.ReadBillingCostSummary(c.Request.Context(), recovery.RequestID)
	if err != nil {
		billingAPIError(c, err)
		return
	}
	if err := audit.ApplyUpstreamCost(costs.UpstreamCostUSD, costs.Complete); err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"recovery": billingRecoveryDTO(recovery), "quote": decode(recovery.QuoteSnapshot), "usage": decode(recovery.UsageSnapshot), "charge": decode(recovery.ChargeSnapshot), "events": events, "attempts": quotes, "audit": audit, "upstream_cost": costs})
}

func ResolveBillingReconciliation(c *gin.Context) {
	var row model.BillingRecovery
	if err := model.DB.Where("request_id = ?", c.Param("id")).First(&row).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	id := row.ID
	var command model.BillingResolution
	if err := common.DecodeJson(c.Request.Body, &command); err != nil {
		billingAPIError(c, model.ErrBillingCommandInvalid)
		return
	}
	recovery, err := model.ResolveBillingReconciliation(c.Request.Context(), id, c.GetInt("id"), command)
	if err != nil {
		billingAPIError(c, err)
		return
	}
	// Accounting success is independent from display delivery. A failed delivery
	// remains retryable and is reported as pending in the returned record.
	if recovery.Status == model.BillingRecoveryStatusSettled || recovery.Status == model.BillingRecoveryStatusRefunded {
		if err := model.ApplyBillingRecoveryConsumption(recovery.RequestID); err != nil {
			logger.LogError(c, "reconciliation delivery remains pending: "+err.Error())
		}
	}
	if err := model.DB.First(recovery, id).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, billingRecoveryDTO(*recovery))
}

func RetryBillingReconciliation(c *gin.Context) {
	var row model.BillingRecovery
	if err := model.DB.Where("request_id = ?", c.Param("id")).First(&row).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	id := row.ID
	var command model.BillingRetryCommand
	if err := common.DecodeJson(c.Request.Body, &command); err != nil {
		billingAPIError(c, model.ErrBillingCommandInvalid)
		return
	}
	recovery, err := model.RecordBillingRetry(c.Request.Context(), id, c.GetInt("id"), command)
	if err != nil {
		billingAPIError(c, err)
		return
	}
	if _, err := service.RetryBillingRecovery(c.Request.Context(), recovery.RequestID); err != nil {
		billingAPIError(c, err)
		return
	}
	if err := model.DB.First(recovery, id).Error; err != nil {
		billingAPIError(c, err)
		return
	}
	common.ApiSuccess(c, billingRecoveryDTO(*recovery))
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

func GetBillingMetrics(c *gin.Context) {
	result, err := model.ReadBillingMetrics(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "billing metrics unavailable"})
		return
	}
	common.ApiSuccess(c, result)
}

func billingRecoveryDTO(r model.BillingRecovery) gin.H {
	return gin.H{"request_id": r.RequestID, "user_id": r.UserID, "token_id": r.TokenID, "state": r.Status, "funding_amount": r.FundingAmount, "token_amount": r.TokenAmount, "version": r.Version, "created_at": r.CreatedAt, "updated_at": r.UpdatedAt, "last_error": r.LastError, "manual_review_required": r.Status == model.BillingRecoveryStatusReconciliation || model.BillingRecoveryUsageUnconfirmed(r.ConsumptionState)}
}
func billingRecoveryDTOs(rows []model.BillingRecovery) []gin.H {
	result := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		result = append(result, billingRecoveryDTO(row))
	}
	return result
}
