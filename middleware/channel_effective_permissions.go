package middleware

import (
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// CanWithAccessToken checks the account grant and the current PAT's grant.
// Browser sessions and legacy tokens retain their existing account-only policy.
func CanWithAccessToken(c *gin.Context, permission authz.Permission) bool {
	if !authz.Can(c.GetInt("id"), c.GetInt("role"), permission) {
		return false
	}
	if _, isToken := c.Get("access_token_ref"); !isToken || c.GetBool("access_token_legacy") {
		return true
	}
	return service.AccessTokenScopeGranted(c.GetStringSlice("access_token_scopes"), service.AccessTokenScopeOf(permission))
}
