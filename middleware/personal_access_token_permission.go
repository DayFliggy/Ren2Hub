package middleware

import (
	"net/http"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

// CurrentUserPermission also protects field-level projections: passing a route's
// ordinary scope cannot borrow the owner's stronger grants. It does not abort a
// response when the caller can receive a less privileged projection instead.
func CurrentUserPermission(c *gin.Context, permission authz.Permission) (bool, error) {
	value, isPAT := c.Get("personal_access_token")
	if isPAT || c.GetBool("use_access_token") {
		token, ok := value.(*model.PersonalAccessToken)
		scope, mapped := service.PATScopeForPermission(permission)
		if ok && token != nil && token.UserID == c.GetInt("id") && mapped && slices.Contains(token.Scopes, scope) {
			return service.CheckPATScope(c.GetInt("id"), c.GetInt("role"), scope)
		}
		return false, nil
	}
	return authz.CanWithDB(model.DB, c.GetInt("id"), c.GetInt("role"), permission)
}

func RequireCurrentUserPermission(c *gin.Context, permission authz.Permission) bool {
	allowed, err := CurrentUserPermission(c, permission)
	if err != nil {
		writeDashboardAuthError(c, err)
		return false
	}
	if allowed {
		return true
	}
	if _, isPAT := c.Get("personal_access_token"); isPAT || c.GetBool("use_access_token") {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "code": "PAT_SCOPE_DENIED", "message": "Personal access token scope does not permit this operation."})
		return false
	}
	c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"success": false, "message": common.TranslateMessage(c, i18n.MsgAuthInsufficientPrivilege)})
	return false
}
