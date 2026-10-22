package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetAffiliateInfo(c *gin.Context) {
	// Reject a bad tz_offset before GetAffiliateInfo runs its thaw transaction.
	loc, ok := viewerLocation(c)
	if !ok {
		return
	}
	result, err := model.GetAffiliateInfo(c.GetInt("id"), loc)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
func TransferAffiliateQuota(c *gin.Context) {
	var request struct {
		OperationID string `json:"operation_id"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		common.ApiError(c, errors.New("expected a single JSON object"))
		return
	}
	result, replayed, err := model.TransferAffiliateQuota(c.GetInt("id"), request.OperationID)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"record": result, "replayed": replayed, "transferred_quota": result.RewardQuota, "wallet_quota": result.WalletAfter})
}
func GetAffiliateRecords(c *gin.Context) {
	page := common.GetPageQuery(c)
	userID := 0
	if value := c.Query("user_id"); value != "" {
		var err error
		userID, err = strconv.Atoi(value)
		if err != nil || userID < 1 {
			common.ApiError(c, errors.New("invalid user_id"))
			return
		}
	}
	rows, total, err := model.ListAffiliateRecords(userID, c.Query("kind"), page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}
func GetAffiliateUserPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		common.ApiError(c, errors.New("invalid user id"))
		return
	}
	result, err := model.GetAffiliateUserPolicy(id, c.GetInt("role"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
func UpdateAffiliateUserPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		common.ApiError(c, errors.New("invalid user id"))
		return
	}
	var request model.AffiliatePolicyUpdate
	if err = c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.UpdateAffiliateUserPolicy(id, c.GetInt("role"), request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	model.RecordOperationAuditLog(c.GetInt("id"), c.GetInt("role"), "Updated user referral policy", c.ClientIP(), "user.affiliate_update", model.AuditFields{"target_user_id": id, "rate_changed": request.RateBps.Set, "rate_bps": request.RateBps.Value, "enabled": request.Enabled, "code_changed": request.Code != nil || request.ResetCode}, auditOperatorInfo(c), &model.AuditRequestInfo{Method: c.Request.Method, Route: c.FullPath(), Status: http.StatusOK, Success: true}, c)
	markAuditLogged(c)
	common.ApiSuccess(c, result)
}

func GetUserAffiliateRecords(c *gin.Context) {
	page := common.GetPageQuery(c)
	rows, total, err := model.ListAffiliateRecords(c.GetInt("id"), c.Query("kind"), page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}

func GetAffiliateInvitees(c *gin.Context) {
	page := common.GetPageQuery(c)
	rows, total, err := model.ListAffiliateInvitees(c.GetInt("id"), c.Query("order"), page)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(rows)
	common.ApiSuccess(c, page)
}
