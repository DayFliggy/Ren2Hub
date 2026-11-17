package controller

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// requestCan 判断本次请求能否行使 permission：先看用户当前角色和权限，
// scoped PAT 还必须自己带有对应 scope。浏览器会话和 legacy PAT 只看用户权限，
// 与路由层 enforceAccessTokenRoute 的规则一致。
func requestCan(c *gin.Context, permission authz.Permission) bool {
	if !authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
		return false
	}
	return accessTokenGrants(c, permission)
}

// accessTokenGrants 只看 scoped PAT 是否带有 permission 对应的 scope；浏览器会话和
// legacy PAT 不受 scope 约束。
func accessTokenGrants(c *gin.Context, permission authz.Permission) bool {
	if c.GetInt("access_token_id") <= 0 {
		return true
	}
	return service.AccessTokenScopeGranted(c.GetStringSlice("access_token_scopes"), service.AccessTokenScopeOf(permission))
}
