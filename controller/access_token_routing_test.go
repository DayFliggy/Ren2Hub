package controller

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestAccessTokenCommonUserRoutingGrantAndEnforcement(t *testing.T) {
	user, _ := setupAccessTokenAudit(t)
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleCommonUser).Error)
	require.NoError(t, model.DB.AutoMigrate(&model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}, &model.Channel{}, &model.Ability{}))
	owned := &model.Token{UserId: user.Id, Key: "routing-owned", Name: "owned", Group: "default"}
	other := &model.Token{UserId: user.Id + 1, Key: "routing-other", Name: "other", Group: "default"}
	require.NoError(t, model.DB.Create(owned).Error)
	require.NoError(t, model.DB.Create(other).Error)
	router := newAccessTokenTestRouter()
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		permission, handler := authz.RoutingRead, GetTokenRouting
		if method == http.MethodPut {
			permission, handler = authz.RoutingWrite, PutTokenRouting
		}
		middleware.DeclareAccessTokenPermissionRoute(method, "/api/token/:id/routing", permission)
		router.Handle(method, "/api/token/:id/routing", middleware.UserAuth(), middleware.RequirePermission(permission), handler)
	}
	identity, browser := createAccessTokenTestSession(t, user.Id, "routing-grant-session")
	response := accessTokenRequest(router, "GET", "/api/user/access_tokens/catalog", browser, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	var catalog struct {
		Data struct {
			Groups []service.AccessTokenCatalogGroup
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &catalog))
	found := false
	for _, group := range catalog.Data.Groups {
		for _, resource := range group.Resources {
			if resource.Resource == authz.ResourceRouting {
				require.Equal(t, service.AccessTokenGroupPersonal, group.Group)
				require.Len(t, resource.Actions, 2)
				found = true
			}
		}
	}
	require.True(t, found, "ordinary users must be able to grant their own routing permissions")
	for _, scope := range []string{"channel:read", "ticket:manage", "user:write"} {
		_, err := service.NormalizeAccessTokenScopes(user.Id, common.RoleCommonUser, []string{scope})
		require.Error(t, err, "routing must not expose administrator scopes: %s", scope)
	}
	proof := issueAccessTokenGenerateProof(t, identity, 0, "routing:read")
	response = accessTokenRequest(router, "POST", "/api/user/access_tokens", browser, proof, `{"name":"routing","scopes":["routing:read"],"expires_at":0}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var created struct {
		Data struct {
			Token string
			Item  struct{ Id int }
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &created))
	require.NotEmpty(t, created.Data.Token)
	path := fmt.Sprintf("/api/token/%d/routing", owned.Id)
	require.Equal(t, http.StatusOK, accessTokenRequest(router, "GET", path, created.Data.Token, "", "").Code)
	require.Equal(t, http.StatusNotFound, accessTokenRequest(router, "GET", fmt.Sprintf("/api/token/%d/routing", other.Id), created.Data.Token, "", "").Code)
	body := `{"mode":"auto","version":0,"entries":[]}`
	require.Equal(t, http.StatusForbidden, accessTokenRequest(router, "PUT", path, created.Data.Token, "", body).Code)
	updatePath := fmt.Sprintf("/api/user/access_tokens/%d", created.Data.Item.Id)
	response = accessTokenRequest(router, "PATCH", updatePath, browser, "", `{"name":"routing","scopes":["routing:write"]}`)
	require.Equal(t, http.StatusForbidden, response.Code, "scope changes must still require step-up")
	proof = issueAccessTokenUpdateProof(t, identity, created.Data.Item.Id, "routing:write")
	response = accessTokenRequest(router, "PATCH", updatePath, browser, proof, `{"name":"routing","scopes":["routing:write"]}`)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, http.StatusForbidden, accessTokenRequest(router, "GET", path, created.Data.Token, "", "").Code)
	require.Equal(t, http.StatusOK, accessTokenRequest(router, "PUT", path, created.Data.Token, "", body).Code)
	require.Equal(t, http.StatusNotFound, accessTokenRequest(router, "PUT", fmt.Sprintf("/api/token/%d/routing", other.Id), created.Data.Token, "", body).Code)
	require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceRouting: {authz.ActionWrite: false}}))
	_, err := service.NormalizeAccessTokenScopes(user.Id, common.RoleCommonUser, []string{"routing:write"})
	require.ErrorIs(t, err, service.ErrAccessTokenScopeForbidden)
	require.Equal(t, http.StatusForbidden, accessTokenRequest(router, "PUT", path, created.Data.Token, "", body).Code, "stored PAT scopes must intersect current permissions")
}
