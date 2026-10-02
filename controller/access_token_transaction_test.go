package controller

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPATLifecycleScopeDigestAndProof(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	body := `{"name":"automation","scopes":["profile:read"]}`
	response := securityEnrollmentRequest("POST", "/api/user/personal-access-tokens", body, "", identity, CreatePersonalAccessToken)
	require.Equal(t, http.StatusForbidden, response.Code)
	var count int64
	require.NoError(t, model.DB.Model(&model.PersonalAccessToken{}).Count(&count).Error)
	assert.Zero(t, count)
	proof := issueSecurityEnrollmentProof(t, identity, service.VerificationOperation{Scope: service.VerificationScopePATCreate}, "password")
	response = securityEnrollmentRequest("POST", "/api/user/personal-access-tokens", body, proof, identity, CreatePersonalAccessToken)
	var result struct {
		Success bool
		Data    struct {
			Token               string
			PersonalAccessToken model.PersonalAccessToken `json:"personal_access_token"`
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
	require.True(t, result.Success, response.Body.String())
	require.Len(t, result.Data.Token, 52)
	token, owner, err := model.ValidatePersonalAccessToken(result.Data.Token)
	require.NoError(t, err)
	assert.Equal(t, user.Id, owner.Id)
	assert.NotEqual(t, result.Data.Token, token.Digest)
	assert.Equal(t, model.PersonalAccessTokenDigest(result.Data.Token), token.Digest)
	assert.InDelta(t, 30*24*3600, token.ExpiresAt-token.CreatedAt, 2)
	list, err := model.ListPersonalAccessTokens(user.Id)
	require.NoError(t, err)
	encoded, err := common.Marshal(list)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), result.Data.Token)
	assert.NotContains(t, string(encoded), token.Digest)
	response = securityEnrollmentRequest("POST", "/api/user/personal-access-tokens", body, proof, identity, CreatePersonalAccessToken)
	assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONSUMED")
	require.NoError(t, model.RevokePersonalAccessToken(identity, token.ID))
	require.NoError(t, model.RevokePersonalAccessToken(identity, token.ID))
	_, _, err = model.ValidatePersonalAccessToken(result.Data.Token)
	assert.ErrorIs(t, err, model.ErrPATInvalid)
	_, _, err = model.CreatePersonalAccessToken(identity, "escalation", []string{"options:write"}, 0)
	assert.ErrorIs(t, err, model.ErrPATInput)
}

func TestPATLimitsAndExpiredOrDeletedOwner(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	var first *model.PersonalAccessToken
	var raw string
	for i := 0; i < model.PersonalAccessTokenLimit; i++ {
		token, secret, err := model.CreatePersonalAccessToken(identity, fmt.Sprint("token-", i), []string{"profile:read"}, 0)
		require.NoError(t, err)
		if i == 0 {
			first, raw = token, secret
		}
	}
	_, _, err := model.CreatePersonalAccessToken(identity, "excess", []string{"profile:read"}, 0)
	require.ErrorIs(t, err, model.ErrPATLimit)
	require.NoError(t, model.DB.Model(first).Update("expires_at", time.Now().Unix()-1).Error)
	_, _, err = model.ValidatePersonalAccessToken(raw)
	require.ErrorIs(t, err, model.ErrPATInvalid)
	_, raw, err = model.CreatePersonalAccessToken(identity, "replacement", []string{"profile:read"}, 0)
	require.NoError(t, err)
	require.NoError(t, model.DB.Delete(user).Error)
	_, _, err = model.ValidatePersonalAccessToken(raw)
	require.ErrorIs(t, err, model.ErrPATInvalid)
}

func TestPATBusinessWriteRechecksSessionAfterProofConsumption(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(fmt.Sprint(revoke), func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			token, raw, err := model.CreatePersonalAccessToken(identity, "existing", []string{"profile:read"}, 0)
			require.NoError(t, err)
			operation := service.VerificationOperation{Scope: service.VerificationScopePATCreate}
			if revoke {
				operation.Scope = service.VerificationScopePATRevoke
				operation.Context = []byte(fmt.Sprintf(`{"id":%d}`, token.ID))
			}
			proof := issueSecurityEnrollmentProof(t, identity, operation, "password")
			authorization, err := service.ConsumeOperationProof(proof, identity, operation)
			require.NoError(t, err)
			_, err = model.RevokeUserSession(user.Id, identity.SessionID, "test-logout")
			require.NoError(t, err)
			if revoke {
				err = model.RevokePersonalAccessToken(authorization.AuthSessionIdentity, token.ID)
			} else {
				_, _, err = model.CreatePersonalAccessToken(authorization.AuthSessionIdentity, "next", []string{"profile:read"}, 0)
			}
			require.ErrorIs(t, err, model.ErrUserSessionInactive)
			_, _, err = model.ValidatePersonalAccessToken(raw)
			require.NoError(t, err)
			var flow model.AuthFlow
			require.NoError(t, model.DB.First(&flow, authorization.ProofID).Error)
			assert.NotNil(t, flow.ConsumedAt)
		})
	}
}

func TestPATScopeFailsClosedAndCannotManageItself(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	_, raw, err := model.CreatePersonalAccessToken(identity, "limited", []string{"profile:read"}, 0)
	require.NoError(t, err)
	middleware.DeclarePATRoute("GET", "/pat/read", "profile:read")
	middleware.DeclarePATRoute("POST", "/pat/write", "tokens:write")
	router := gin.New()
	router.GET("/pat/read", middleware.UserAuth(), func(c *gin.Context) { c.Status(204) })
	router.POST("/pat/write", middleware.UserAuth(), func(c *gin.Context) { c.Status(204) })
	router.GET("/pat/undeclared", middleware.UserAuth(), func(c *gin.Context) { c.Status(204) })
	router.POST("/pat/self", middleware.UserAuth(), middleware.BrowserSessionOnly(), CreatePersonalAccessToken)
	for _, test := range []struct {
		method, path string
		status       int
	}{{"GET", "/pat/read", 204}, {"POST", "/pat/write", 403}, {"GET", "/pat/undeclared", 403}, {"POST", "/pat/self", 403}} {
		response := auditRequest(router, test.method, test.path, raw)
		assert.Equal(t, test.status, response.Code)
	}
	assert.False(t, strings.Contains(raw, " "))
}

func TestPATFinalSlotIsAtomic(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	for i := 0; i < model.PersonalAccessTokenLimit-1; i++ {
		_, _, err := model.CreatePersonalAccessToken(identity, fmt.Sprint("initial-", i), []string{"profile:read"}, 0)
		require.NoError(t, err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			_, _, err := model.CreatePersonalAccessToken(identity, "concurrent", []string{"profile:read"}, 0)
			results <- err
		}()
	}
	close(start)
	success, limited := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else {
			require.ErrorIs(t, err, model.ErrPATLimit)
			limited++
		}
	}
	assert.Equal(t, 1, success)
	assert.Equal(t, 1, limited)
}

func TestPATIssuanceAndUseReadCurrentPermissions(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}))
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = wasMaster })
	require.NoError(t, authz.Init(model.DB))
	require.NoError(t, model.DB.Model(user).Update("role", common.RoleAdminUser).Error)
	require.NoError(t, authz.SetUserPermissions(user.Id, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionSensitiveWrite: true}}))
	_, _, err := model.CreatePersonalAccessToken(identity, "granted", []string{"channels:sensitive_write"}, 0, service.ValidatePATScopesWithTx)
	require.NoError(t, err)
	require.NoError(t, model.DB.Where("ptype = ? AND v0 = ? AND v1 = ? AND v2 = ?", "p", authz.UserSubject(user.Id), authz.ResourceChannel, authz.ActionSensitiveWrite).Delete(&model.CasbinRule{}).Error)
	require.True(t, authz.Can(user.Id, common.RoleAdminUser, authz.ChannelSensitiveWrite), "simulate the reload gap after a committed revocation")
	allowed, err := service.CheckPATScope(user.Id, common.RoleAdminUser, "channels:sensitive_write")
	require.NoError(t, err)
	assert.False(t, allowed)
	_, _, err = model.CreatePersonalAccessToken(identity, "stale", []string{"channels:sensitive_write"}, 0, service.ValidatePATScopesWithTx)
	require.ErrorIs(t, err, service.ErrVerificationForbidden)
	var count int64
	require.NoError(t, model.DB.Model(&model.PersonalAccessToken{}).Where("user_id = ?", user.Id).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
