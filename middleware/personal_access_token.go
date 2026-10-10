package middleware

import (
	"net/http"
	"slices"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

var patRouteScopes sync.Map

// DeclarePATRoute is called beside router registration. Missing declarations
// fail closed even for root-owned PATs; browser sessions keep existing access.
func DeclarePATRoute(method, path, scope string) {
	if _, known := model.PATScopeRoles[scope]; !known {
		panic("unknown PAT scope: " + scope)
	}
	patRouteScopes.Store(method+" "+path, scope)
}

func authorizePATRoute(c *gin.Context, user *model.UserBase) bool {
	value, exists := c.Get("personal_access_token")
	if !exists {
		return true
	}
	token, ok := value.(*model.PersonalAccessToken)
	required, declared := patRouteScopes.Load(c.Request.Method + " " + c.FullPath())
	allowed := false
	if ok && declared && slices.Contains(token.Scopes, required.(string)) {
		var err error
		allowed, err = service.CheckPATScope(user.Id, user.Role, required.(string))
		if err != nil {
			writeDashboardAuthError(c, err)
			return false
		}
	}
	if !allowed {
		abortWithWebMessage(c, http.StatusForbidden, "PAT_SCOPE_DENIED", common.NewMessage("Personal access token scope does not permit this route."))
		return false
	}
	return true
}

func BrowserSessionOnly() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := GetSessionAuthIdentity(c); !ok {
			abortWithWebMessage(c, http.StatusForbidden, "BROWSER_SESSION_REQUIRED", common.NewMessage("A browser session is required."))
			return
		}
		c.Next()
	}
}
