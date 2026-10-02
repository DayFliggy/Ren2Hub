package controller

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestDiagnosticOwnershipProjectionAndTokenCorrelation(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.GatewayRequestDiagnostic{}))
	for i := 1; i <= 3; i++ {
		owner := user.Id
		if i == 3 {
			owner++
		}
		decision := service.NewRouteDecision(fmt.Sprintf("request-%d", i), service.RouteSourceAutoLab, "model", 1)
		decision.UserID, decision.TokenID, decision.TraceID = owner, i, "shared"
		decision.Attempts = []service.RouteDecisionAttempt{{Sequence: 1, AttemptID: "attempt", CredentialID: "internal-credential", ChannelID: 42, Origin: service.AttemptOriginProvider, Acceptance: service.AttemptAccepted, RetryPermission: service.AttemptRetryPolicy, Success: true, SendStartedAt: 1, FinishedAt: 2}}
		require.NoError(t, service.PersistRouteDecision(context.Background(), decision))
	}
	response := securityEnrollmentRequest("GET", "/api/log/self/requests/request-1/diagnostics", "", "", identity, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "request_id", Value: "request-1"}}
		GetSelfRequestDiagnostic(c)
	})
	require.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), "internal-credential")
	assert.NotContains(t, response.Body.String(), "channel_id")
	assert.Contains(t, response.Body.String(), `"response_committed":false`)
	response = securityEnrollmentRequest("GET", "/api/log/self/requests/request-3/diagnostics", "", "", identity, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "request_id", Value: "request-3"}}
		GetSelfRequestDiagnostic(c)
	})
	assert.Equal(t, http.StatusNotFound, response.Code)
	response = securityEnrollmentRequest("GET", "/api/log/self/request-diagnostics?trace_id=shared&token_id=1", "", "", identity, ListSelfRequestDiagnostics)
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "request-1")
	assert.NotContains(t, response.Body.String(), "request-2")
	assert.NotContains(t, response.Body.String(), "request-3")
	response = securityEnrollmentRequest("GET", "/api/log/self/request-diagnostics?trace_id=shared", "", "", identity, ListSelfRequestDiagnostics)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	response = securityEnrollmentRequest("GET", "/api/log/requests/request-1/diagnostics", "", "", identity, func(c *gin.Context) {
		c.Set("role", common.RoleRootUser)
		c.Params = gin.Params{{Key: "request_id", Value: "request-1"}}
		GetAdminRequestDiagnostic(c)
	})
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "internal-credential")
	require.NoError(t, model.DB.Model(&model.GatewayRequestDiagnostic{}).Where("request_id = ?", "request-1").Update("created_at", time.Now().Add(-8*24*time.Hour).Unix()).Error)
	response = securityEnrollmentRequest("GET", "/api/log/self/requests/request-1/diagnostics", "", "", identity, func(c *gin.Context) {
		c.Params = gin.Params{{Key: "request_id", Value: "request-1"}}
		GetSelfRequestDiagnostic(c)
	})
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestRequestDiagnosticRejectsUnboundedOrMissingCorrelationInput(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	for _, query := range []string{"?trace_id=a", "?thread_id=a&token_id=-1", "?trace_id=" + strings.Repeat("x", 129) + "&token_id=1", "?thread_id=with%20space&token_id=1"} {
		response := securityEnrollmentRequest("GET", "/api/log/self/request-diagnostics"+query, "", "", identity, ListSelfRequestDiagnostics)
		assert.Equal(t, http.StatusBadRequest, response.Code)
	}
}

func TestRequestDiagnosticAdminProjectionIntersectsPATScopes(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.GatewayRequestDiagnostic{}, &model.CasbinRule{}, &model.AuthzRole{}))
	decision := service.NewRouteDecision("scope-projection", service.RouteSourceAutoLab, "model", 1)
	decision.UserID, decision.TokenID = user.Id, 1
	decision.Attempts = []service.RouteDecisionAttempt{{Sequence: 1, AttemptID: "attempt", ChannelID: 42, CredentialID: "private-credential"}}
	require.NoError(t, service.PersistRouteDecision(context.Background(), decision))
	for _, test := range []struct {
		name     string
		scopes   []string
		owner    int
		role     int
		internal bool
	}{
		{"ops alone", []string{"ops:read"}, user.Id, common.RoleRootUser, false},
		{"explicit channel scope", []string{"ops:read", "channels:read"}, user.Id, common.RoleRootUser, true},
		{"wrong owner", []string{"ops:read", "channels:read"}, user.Id + 1, common.RoleRootUser, false},
		{"missing credential", nil, user.Id, common.RoleRootUser, false},
		{"current permission absent", []string{"ops:read", "channels:read"}, user.Id, common.RoleAdminUser, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, list := range []bool{false, true} {
				response := securityEnrollmentRequest("GET", "/api/log/request-diagnostics", "", "", identity, func(c *gin.Context) {
					c.Set("role", test.role)
					c.Set("use_access_token", true)
					if test.scopes != nil {
						c.Set("personal_access_token", &model.PersonalAccessToken{UserID: test.owner, Scopes: test.scopes})
					}
					if list {
						ListAdminRequestDiagnostics(c)
					} else {
						c.Params = gin.Params{{Key: "request_id", Value: "scope-projection"}}
						GetAdminRequestDiagnostic(c)
					}
				})
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				assert.Equal(t, test.internal, strings.Contains(response.Body.String(), "private-credential"))
				assert.Equal(t, test.internal, strings.Contains(response.Body.String(), "channel_id"))
			}
		})
	}

	// A granted PAT must lose the private projection as soon as the owner's
	// resource permission is withdrawn, without waiting for a cached enforcer.
	rule := model.CasbinRule{Ptype: "p", V0: authz.UserSubject(user.Id), V1: authz.ResourceChannel, V2: authz.ActionRead, V3: authz.EffectAllow}
	require.NoError(t, model.DB.Create(&rule).Error)
	for _, expected := range []bool{true, false} {
		response := securityEnrollmentRequest("GET", "/api/log/request-diagnostics", "", "", identity, func(c *gin.Context) {
			c.Set("role", common.RoleAdminUser)
			c.Set("personal_access_token", &model.PersonalAccessToken{UserID: user.Id, Scopes: []string{"ops:read", "channels:read"}})
			ListAdminRequestDiagnostics(c)
		})
		require.Equal(t, http.StatusOK, response.Code)
		assert.Equal(t, expected, strings.Contains(response.Body.String(), "private-credential"))
		require.NoError(t, model.DB.Model(&rule).Update("v3", authz.EffectDeny).Error)
	}
}
