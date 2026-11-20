package controller

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// accessTokenGrants 只看 scoped PAT 是否带有 permission 对应的 scope；浏览器会话和
// legacy PAT 不受 scope 约束。
func accessTokenGrants(c *gin.Context, permission authz.Permission) bool {
	if c.GetInt("access_token_id") <= 0 {
		return true
	}
	return service.AccessTokenScopeGranted(c.GetStringSlice("access_token_scopes"), service.AccessTokenScopeOf(permission))
}
