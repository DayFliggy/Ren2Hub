package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
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
		common.SysError(common.LogText("failed to get token by key: %s", err.Error()))
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
		common.ApiErrorT(c, "Invalid conversion policy")
		return
	}
	if len(token.Name) > 50 {
		common.ApiErrorT(c, "Token name is too long")
		return
	}
	// 非无限额度时，检查额度值是否超出有效范围
	if !token.UnlimitedQuota {
		if token.RemainQuota < 0 {
			common.ApiErrorT(c, "Quota value cannot be negative")
			return
		}
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorT(c, "Failed to generate token")
		common.SysLog(common.LogText("failed to generate token key: %s", err.Error()))
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
		common.ApiErrorT(c, "Invalid token type")
		return
	}
	err = cleanToken.InsertWithRouteMode(mode)
	if err != nil {
		if errors.Is(err, model.ErrTokenCountLimit) {
			common.ApiErrorWithCode(c, common.NewMessage("Maximum number of tokens reached ({{max}})", map[string]any{"max": operation_setting.GetMaxUserTokens()}), "TOKEN_COUNT_LIMIT")
			return
		}
		if errors.Is(err, model.ErrTokenCreationRateLimit) {
			common.ApiErrorStatus(c, http.StatusTooManyRequests, common.NewMessage("Tokens are being created too often, please try again later"), gin.H{"code": "TOKEN_CREATION_RATE_LIMIT"})
			return
		}
		if errors.Is(err, model.ErrBillingEventConflict) || errors.Is(err, model.ErrBillingVersionConflict) {
			common.ApiErrorStatus(c, http.StatusConflict, err)
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
		if known := tokenRequestFieldCase(field); known != "" && known != field {
			// encoding/json would decode the value case-insensitively while
			// the presence check is exact, so the setting would be dropped.
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Unknown field {{field}}; use {{name}}", map[string]any{"field": strconv.Quote(field), "name": strconv.Quote(known)}))
			return
		}
		present[field] = true
	}
	token := request.Token
	setTokenAuditParams(c, model.AuditFields{"id": token.Id, "name": token.Name, "status": token.Status, "operation_id": request.OperationID, "expected_version": request.ExpectedVersion})
	if len(token.Name) > 50 {
		common.ApiErrorT(c, "Token name is too long")
		return
	}
	if statusOnly == "" && token.ConversionPolicy != "" && !validTokenConversionPolicy(token.ConversionPolicy) {
		common.ApiErrorT(c, "Invalid conversion policy")
		return
	}
	token.UserId = userId
	err = token.UpdateFromRequest(request.OperationID, request.ExpectedVersion, statusOnly != "", present)
	if err != nil {
		if errors.Is(err, model.ErrBillingEventConflict) || errors.Is(err, model.ErrBillingVersionConflict) {
			common.ApiErrorStatus(c, http.StatusConflict, err)
			return
		}
		common.ApiError(c, err)
		return
	}
	common.SetContextKey(c, "token_audit_succeeded", true)
	writeTokenResponse(c, &token)
}

// tokenRequestFields are the JSON names a token update decodes, taken from the
// request type so the list cannot drift from it.
var tokenRequestFields = jsonFieldNames(reflect.TypeOf(tokenRequest{}))

// tokenRequestFieldCase returns the declared name that matches field ignoring
// case, or "" for a field the request does not declare.
func tokenRequestFieldCase(field string) string {
	for _, name := range tokenRequestFields {
		if strings.EqualFold(name, field) {
			return name
		}
	}
	return ""
}

func jsonFieldNames(t reflect.Type) []string {
	var names []string
	for i := range t.NumField() {
		field := t.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.Anonymous && name == "" && field.Type.Kind() == reflect.Struct {
			names = append(names, jsonFieldNames(field.Type)...)
			continue
		}
		if !field.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = field.Name
		}
		names = append(names, name)
	}
	return names
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
		common.ApiErrorT(c, "Invalid parameters")
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
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	if len(tokenBatch.Ids) > 100 {
		common.ApiErrorT(c, "Too many items in batch request, maximum is {{max}}", map[string]any{"max": 100})
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
