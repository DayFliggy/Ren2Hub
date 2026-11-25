package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

var buildModelCatalog = service.BuildModelCatalog

// GetModelCatalog 带 token_id 时按那把 Key 的模型限制和路由投影，与
// /api/token/:id/routing/channels 相同，所以 scoped PAT 另需 routing:read；路由规则表
// 对本接口只要求 profile:read。
func GetModelCatalog(c *gin.Context) {
	tokenID := 0
	if value := c.Query("token_id"); value != "" {
		id, err := strconv.Atoi(value)
		if err != nil || id <= 0 {
			common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("invalid token_id"))
			return
		}
		tokenID = id
		if !accessTokenGrants(c, authz.RoutingRead) {
			scope := service.AccessTokenScopeOf(authz.RoutingRead)
			common.ApiErrorStatus(c, http.StatusForbidden, common.NewMessage("The access token lacks the required permission: {{scope}}", map[string]any{"scope": scope}), gin.H{"code": "ACCESS_TOKEN_SCOPE_DENIED"})
			return
		}
	}
	result, err := buildModelCatalog(c.Request.Context(), c.GetInt("id"), tokenID)
	if err != nil {
		if errors.Is(err, service.ErrCatalogConfigChanging) {
			common.ApiErrorStatus(c, http.StatusServiceUnavailable, common.NewMessage("Model catalog is temporarily updating, please retry"), gin.H{"code": "CATALOG_BUSY"})
			return
		}
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}
