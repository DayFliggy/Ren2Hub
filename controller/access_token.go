package controller

import (
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func GetPersonalAccessTokenScopes(c *gin.Context) {
	var owner model.User
	if err := model.DB.Select("id", "role").First(&owner, c.GetInt("id")).Error; err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	scopes := make([]string, 0, len(model.PATScopeRoles))
	for scope := range model.PATScopeRoles {
		allowed, err := service.CheckPATScope(owner.Id, owner.Role, scope)
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		if allowed {
			scopes = append(scopes, scope)
		}
	}
	slices.Sort(scopes)
	common.ApiSuccess(c, gin.H{"scopes": scopes, "limit": model.PersonalAccessTokenLimit, "default_lifetime_days": 30})
}

func UpdatePersonalAccessToken(c *gin.Context) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		writeSecurityOperationError(c, model.ErrPATInvalid)
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		writeSecurityOperationError(c, model.ErrPATNotFound)
		return
	}
	var fields map[string]json.RawMessage
	if common.DecodeJson(c.Request.Body, &fields) != nil || len(fields) < 1 || len(fields) > 2 {
		writeSecurityOperationError(c, model.ErrPATInput)
		return
	}
	for key := range fields {
		if key != "name" && key != "scopes" {
			writeSecurityOperationError(c, model.ErrPATInput)
			return
		}
	}
	var name *string
	if raw, supplied := fields["name"]; supplied {
		var value string
		if common.Unmarshal(raw, &value) != nil {
			writeSecurityOperationError(c, model.ErrPATInput)
			return
		}
		value, err = model.NormalizePATName(value)
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		name = &value
	}
	if _, err := model.GetActivePersonalAccessToken(identity.UserID, id); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	var scopes []string
	if raw, supplied := fields["scopes"]; supplied {
		if common.Unmarshal(raw, &scopes) != nil {
			writeSecurityOperationError(c, model.ErrPATInput)
			return
		}
		scopes, err = model.NormalizePATScopes(scopes, c.GetInt("role"))
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		for _, scope := range scopes {
			allowed, err := service.CheckPATScope(identity.UserID, c.GetInt("role"), scope)
			if err != nil {
				writeSecurityOperationError(c, err)
				return
			}
			if !allowed {
				writeSecurityOperationError(c, service.ErrVerificationForbidden)
				return
			}
		}
		context, err := common.Marshal(service.PATUpdateContext{ID: id, Scopes: scopes})
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePATUpdate, Context: context})
		if authorization == nil {
			return
		}
		identity = authorization.AuthSessionIdentity
	}
	token, err := model.UpdatePersonalAccessToken(identity, id, name, scopes, service.ValidatePATScopesWithTx)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	action := "pat.rename"
	if scopes != nil {
		action = "pat.update"
	}
	recordUserSecurityAudit(c, identity.UserID, action, map[string]any{"pat_id": id})
	common.ApiSuccess(c, token)
}

func ListPersonalAccessTokens(c *gin.Context) {
	items, err := model.ListPersonalAccessTokens(c.GetInt("id"))
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	common.ApiSuccess(c, items)
}

func CreatePersonalAccessToken(c *gin.Context) {
	var request struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresAt int64    `json:"expires_at"`
	}
	if common.DecodeJson(c.Request.Body, &request) != nil {
		common.ApiErrorT(c, "Invalid personal access token input")
		return
	}
	if _, _, _, err := model.NormalizePATInput(request.Name, request.Scopes, request.ExpiresAt, c.GetInt("role")); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	for _, scope := range request.Scopes {
		allowed, err := service.CheckPATScope(c.GetInt("id"), c.GetInt("role"), scope)
		if err != nil {
			writeSecurityOperationError(c, err)
			return
		}
		if !allowed {
			common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("Scope exceeds current permissions."), gin.H{"code": "PAT_SCOPE_DENIED"})
			c.Abort()
			return
		}
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePATCreate})
	if authorization == nil {
		return
	}
	token, raw, err := model.CreatePersonalAccessToken(authorization.AuthSessionIdentity, request.Name, request.Scopes, request.ExpiresAt, service.ValidatePATScopesWithTx)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, c.GetInt("id"), "pat.create", map[string]any{"pat_id": token.ID})
	common.ApiSuccess(c, gin.H{"token": raw, "personal_access_token": token})
}

func RevokePersonalAccessToken(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorT(c, "Invalid personal access token ID")
		return
	}
	context, err := common.Marshal(gin.H{"id": id})
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	authorization := middleware.RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopePATRevoke, Context: context})
	if authorization == nil {
		return
	}
	if err := model.RevokePersonalAccessToken(authorization.AuthSessionIdentity, id); err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, c.GetInt("id"), "pat.revoke", map[string]any{"pat_id": id})
	common.ApiSuccess(c, nil)
}

func GetAuditLogs(c *gin.Context) {
	page := common.GetPageQuery(c)
	if page.Page < 1 || page.PageSize < 1 || page.Page > 100000000 {
		common.ApiErrorT(c, "Invalid audit pagination")
		return
	}
	filter := model.AuditLogFilter{Username: c.Query("username"), Category: c.Query("category"), TokenRef: c.Query("token_ref"), ExcludeTokenRef: c.Query("exclude_token_ref"), RequestId: c.Query("request_id"), Keyword: strings.TrimSpace(c.Query("keyword"))}
	if utf8.RuneCountInString(filter.Keyword) > 128 {
		common.ApiErrorT(c, "Invalid audit keyword")
		return
	}
	viewerRole := c.GetInt("role")
	if c.FullPath() == "/api/audit/self" {
		filter.UserId = c.GetInt("id")
		filter.Username = ""
		filter.SelfView = true
	}
	if !model.ValidAuditCategory(filter.Category) || !model.ValidTokenFingerprint(filter.TokenRef) || !model.ValidTokenFingerprint(filter.ExcludeTokenRef) {
		common.ApiErrorT(c, "Invalid audit filters")
		return
	}
	for name, target := range map[string]*int64{"start_timestamp": &filter.StartTimestamp, "end_timestamp": &filter.EndTimestamp} {
		if raw := c.Query(name); raw != "" {
			parsed, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || parsed < 0 {
				common.ApiErrorT(c, "Invalid audit time range")
				return
			}
			*target = parsed
		}
	}
	if filter.EndTimestamp > 0 && filter.EndTimestamp < filter.StartTimestamp {
		common.ApiErrorT(c, "Invalid audit time range")
		return
	}
	if raw := c.Query("success"); raw != "" {
		if raw != "true" && raw != "false" {
			common.ApiErrorT(c, "Invalid audit result")
			return
		}
		success := raw == "true"
		filter.Success = &success
	}
	logs, total, err := model.GetAuditLogs(filter, page.GetStartIdx(), page.GetPageSize(), viewerRole)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetItems(logs)
	page.SetTotal(int(total))
	common.ApiSuccess(c, page)
}
