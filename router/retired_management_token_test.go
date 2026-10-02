package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetiredManagementTokenRoutesCannotBecomeUserLookups(t *testing.T) {
	setupRelayRouterTestDB(t)
	owner := &model.User{Username: "retired-token-route", AffCode: "retired-token-route", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(owner).Error)
	credential := createRouterBrowserSession(t, owner.Id)
	engine := gin.New()
	SetApiRouter(engine)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete, http.MethodPut} {
		for _, path := range []string{"/api/user/token", "/api/user/token/status"} {
			request := httptest.NewRequest(method, path, nil)
			request.Header.Set("Authorization", "Bearer "+credential)
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, http.StatusNotFound, response.Code, method+" "+path)
		}
	}
	for _, id := range []string{"0", "-1", "invalid", "999999999999999999999999"} {
		request := httptest.NewRequest(http.MethodGet, "/api/user/"+id, nil)
		request.Header.Set("Authorization", "Bearer "+credential)
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, http.StatusNotFound, response.Code, id)
	}
}
