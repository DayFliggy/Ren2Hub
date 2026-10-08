package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/gin-gonic/gin"
)

type tokenRequest struct {
	OperationID     string `json:"operation_id"`
	ExpectedVersion *int64 `json:"expected_version"`
	model.Token
	Type string `json:"type"`
}

type tokenResponse struct {
	*model.Token
	Type string `json:"type"`
}

func (response tokenResponse) MarshalJSON() ([]byte, error) {
	type tokenJSON model.Token
	var available *common.Quota
	if !response.UnlimitedQuota {
		available = &response.RemainQuota
	}
	return common.Marshal(struct {
		tokenJSON
		Available *common.Quota `json:"available_quota"`
		Type      string        `json:"type"`
	}{tokenJSON: tokenJSON(*response.Token), Available: available, Type: response.Type})
}

func buildMaskedTokenResponses(tokens []*model.Token) ([]*tokenResponse, error) {
	ids := make([]int, 0, len(tokens))
	for _, token := range tokens {
		if token != nil {
			ids = append(ids, token.Id)
		}
	}
	profiles := make([]model.UserRouteProfile, 0)
	if len(ids) > 0 {
		if err := model.DB.Select("token_id", "mode").Where("token_id IN ?", ids).Find(&profiles).Error; err != nil {
			return nil, err
		}
	}
	modes := make(map[int]string, len(profiles))
	for _, profile := range profiles {
		modes[profile.TokenID] = profile.Mode
	}
	responses := make([]*tokenResponse, 0, len(tokens))
	for _, token := range tokens {
		if token == nil {
			continue
		}
		masked := *token
		if masked.ConversionPolicy == "" {
			masked.ConversionPolicy = string(types.ConversionLossPolicySafe)
		}
		masked.Key = token.GetMaskedKey()
		tokenType := "auto"
		if modes[token.Id] == model.RouteModeManual {
			tokenType = "manual"
		}
		responses = append(responses, &tokenResponse{Token: &masked, Type: tokenType})
	}
	return responses, nil
}

func writeTokenResponse(c *gin.Context, token *model.Token) {
	responses, err := buildMaskedTokenResponses([]*model.Token{token})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, responses[0])
}

func GetAllTokens(c *gin.Context) {
	userId := c.GetInt("id")
	pageInfo := common.GetPageQuery(c)
	tokens, err := model.GetAllUserTokens(userId, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	total, _ := model.CountUserTokens(userId)
	pageInfo.SetTotal(int(total))
	responses, err := buildMaskedTokenResponses(tokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetItems(responses)
	common.ApiSuccess(c, pageInfo)
}

func SearchTokens(c *gin.Context) {
	userId := c.GetInt("id")
	keyword := c.Query("keyword")
	token := c.Query("token")

	pageInfo := common.GetPageQuery(c)

	tokens, total, err := model.SearchUserTokens(userId, keyword, token, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	responses, err := buildMaskedTokenResponses(tokens)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetItems(responses)
	common.ApiSuccess(c, pageInfo)
}

func GetToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	writeTokenResponse(c, token)
}

func GetTokenKey(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token, err := model.GetTokenByIds(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setTokenAuditParams(c, model.AuditFields{"id": token.Id, "name": token.Name})
	common.SetContextKey(c, "token_audit_succeeded", true)
	common.ApiSuccess(c, gin.H{
		"key": token.GetFullKey(),
	})
}

func tokenBudgetSummary(token *model.Token) (any, any, error) {
	if token.UnlimitedQuota {
		return nil, nil, nil
	}
	total, err := common.AddQuota(token.RemainQuota, token.ReservedQuota)
	if err != nil {
		return nil, nil, err
	}
	total, err = common.AddQuota(total, token.UsedQuota)
	if err != nil {
		return nil, nil, err
	}
	return token.RemainQuota, total, nil
}

func GetTokenStatus(c *gin.Context) {
	tokenId := c.GetInt("token_id")
	userId := c.GetInt("id")
	token, err := model.GetTokenByIds(tokenId, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}
	available, total, err := tokenBudgetSummary(token)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"object":          "credit_summary",
		"total_granted":   total,
		"total_used":      token.UsedQuota,
		"reserved_quota":  token.ReservedQuota,
		"total_available": available,
		"expires_at":      expiredAt * 1000,
	})
}

func GetTokenUsage(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "No Authorization header",
		})
		return
	}

	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "Invalid Bearer token",
		})
		return
	}
	tokenKey := parts[1]

	token, err := model.GetTokenByKey(strings.TrimPrefix(tokenKey, "sk-"), false)
	if err != nil {
		common.SysError("failed to get token by key: " + err.Error())
		common.ApiErrorI18n(c, i18n.MsgTokenGetInfoFailed)
		return
	}

	expiredAt := token.ExpiredTime
	if expiredAt == -1 {
		expiredAt = 0
	}

	available, total, err := tokenBudgetSummary(token)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    true,
		"message": "ok",
		"data": gin.H{
			"object":               "token_usage",
			"name":                 token.Name,
			"total_granted":        total,
			"reserved_quota":       token.ReservedQuota,
			"total_used":           token.UsedQuota,
			"total_available":      available,
			"unlimited_quota":      token.UnlimitedQuota,
			"model_limits":         token.GetModelLimitsMap(),
			"model_limits_enabled": token.ModelLimitsEnabled,
			"expires_at":           expiredAt,
		},
	})
}

func AddToken(c *gin.Context) {
	request := tokenRequest{}
	err := c.ShouldBindJSON(&request)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	token := request.Token
	setTokenAuditParams(c, model.AuditFields{"name": token.Name, "unlimited_quota": token.UnlimitedQuota, "available_quota": token.RemainQuota})
	if token.ConversionPolicy == "" {
		token.ConversionPolicy = string(types.ConversionLossPolicySafe)
	}
	if !validTokenConversionPolicy(token.ConversionPolicy) {
		common.ApiError(c, fmt.Errorf("invalid conversion_policy"))
		return
	}
	if len(token.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	// 非无限额度时，检查额度值是否超出有效范围
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.ApiErrorI18n(c, i18n.MsgTokenQuotaNegative)
			return
		}
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		common.SysLog("failed to generate token key: " + err.Error())
		return
	}
	cleanToken := model.Token{
		UserId:             c.GetInt("id"),
		Name:               token.Name,
		Key:                key,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        token.ExpiredTime,
		RemainQuota:        token.RemainQuota,
		UnlimitedQuota:     token.UnlimitedQuota,
		ModelLimitsEnabled: token.ModelLimitsEnabled,
		ModelLimits:        token.ModelLimits,
		AllowIps:           token.AllowIps,
		ConversionPolicy:   token.ConversionPolicy,
	}
	mode := model.RouteModeAutoLab
	if request.Type == "manual" {
		mode = model.RouteModeManual
	} else if request.Type != "" && request.Type != "auto" {
		common.ApiErrorMsg(c, "invalid token type")
		return
	}
	err = cleanToken.InsertWithRouteMode(mode)
	if err != nil {
		if errors.Is(err, model.ErrTokenCountLimit) {
			c.JSON(http.StatusOK, gin.H{"success": false, "code": "TOKEN_COUNT_LIMIT", "message": fmt.Sprintf("已达到最大令牌数量限制 (%d)", operation_setting.GetMaxUserTokens())})
			return
		}
		if errors.Is(err, model.ErrTokenCreationRateLimit) {
			c.JSON(http.StatusTooManyRequests, gin.H{"success": false, "code": "TOKEN_CREATION_RATE_LIMIT", "message": "令牌创建过于频繁，请稍后重试"})
			return
		}
		if errors.Is(err, model.ErrBillingEventConflict) || errors.Is(err, model.ErrBillingVersionConflict) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	setTokenAuditParams(c, model.AuditFields{"id": cleanToken.Id})
	common.SetContextKey(c, "token_audit_succeeded", true)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func DeleteToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	userId := c.GetInt("id")
	err := model.DeleteTokenById(id, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.SetContextKey(c, "token_audit_succeeded", true)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
	})
}

func UpdateToken(c *gin.Context) {
	userId := c.GetInt("id")
	statusOnly := c.Query("status_only")
	var fields map[string]json.RawMessage
	if err := c.ShouldBindJSON(&fields); err != nil {
		common.ApiError(c, err)
		return
	}
	request := tokenRequest{}
	encoded, err := common.Marshal(fields)
	if err == nil {
		err = common.Unmarshal(encoded, &request)
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	present := make(map[string]bool, len(fields))
	for field := range fields {
		present[field] = true
	}
	token := request.Token
	setTokenAuditParams(c, model.AuditFields{"id": token.Id, "name": token.Name, "status": token.Status, "operation_id": request.OperationID, "expected_version": request.ExpectedVersion})
	if len(token.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if statusOnly == "" && token.ConversionPolicy != "" && !validTokenConversionPolicy(token.ConversionPolicy) {
		common.ApiError(c, fmt.Errorf("invalid conversion_policy"))
		return
	}
	token.UserId = userId
	err = token.UpdateFromRequest(request.OperationID, request.ExpectedVersion, statusOnly != "", present)
	if err != nil {
		if errors.Is(err, model.ErrBillingEventConflict) || errors.Is(err, model.ErrBillingVersionConflict) {
			c.JSON(http.StatusConflict, gin.H{"success": false, "message": err.Error()})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.SetContextKey(c, "token_audit_succeeded", true)
	writeTokenResponse(c, &token)
}

func validTokenConversionPolicy(policy string) bool {
	return policy == string(types.ConversionLossPolicySafe) || policy == string(types.ConversionLossPolicyAllow) || policy == string(types.ConversionLossPolicyStrict)
}

type TokenBatch struct {
	Ids []int `json:"ids"`
}

func DeleteTokenBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	setTokenAuditParams(c, model.AuditFields{"requested_count": len(tokenBatch.Ids)})
	userId := c.GetInt("id")
	count, err := model.BatchDeleteTokens(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	setTokenAuditParams(c, model.AuditFields{"deleted_count": count})
	common.SetContextKey(c, "token_audit_succeeded", true)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    count,
	})
}

func GetTokenKeysBatch(c *gin.Context) {
	tokenBatch := TokenBatch{}
	if err := c.ShouldBindJSON(&tokenBatch); err != nil || len(tokenBatch.Ids) == 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	if len(tokenBatch.Ids) > 100 {
		common.ApiErrorI18n(c, i18n.MsgBatchTooMany, map[string]any{"Max": 100})
		return
	}
	setTokenAuditParams(c, model.AuditFields{"ids": tokenBatch.Ids, "requested_count": len(tokenBatch.Ids)})
	userId := c.GetInt("id")
	tokens, err := model.GetTokenKeysByIds(tokenBatch.Ids, userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	keysMap := make(map[int]string)
	for _, t := range tokens {
		keysMap[t.Id] = t.GetFullKey()
	}
	setTokenAuditParams(c, model.AuditFields{"count": len(keysMap)})
	common.SetContextKey(c, "token_audit_succeeded", true)
	common.ApiSuccess(c, gin.H{"keys": keysMap})
}
