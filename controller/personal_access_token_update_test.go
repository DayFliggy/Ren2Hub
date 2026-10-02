package controller

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPATUpdateNameGrantAndBoundProof(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	token, raw, err := model.CreatePersonalAccessToken(identity, "initial", []string{"profile:read"}, 0)
	require.NoError(t, err)
	handler := func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(token.ID)}}
		UpdatePersonalAccessToken(c)
	}
	response := securityEnrollmentRequest("PATCH", "/pat", `{"name":"renamed"}`, "", identity, handler)
	require.Equal(t, http.StatusOK, response.Code)
	stored, err := model.GetActivePersonalAccessToken(identity.UserID, token.ID)
	require.NoError(t, err)
	assert.Equal(t, "renamed", stored.Name)
	assert.Equal(t, token.Digest, stored.Digest)
	assert.Equal(t, token.ExpiresAt, stored.ExpiresAt)
	assert.NotContains(t, response.Body.String(), raw)
	assert.NotContains(t, response.Body.String(), stored.Digest)

	context := []byte(fmt.Sprintf(`{"id":%d,"scopes":["tokens:read","profile:read"]}`, token.ID))
	operation := service.VerificationOperation{Scope: service.VerificationScopePATUpdate, Context: context}
	proof := issueSecurityEnrollmentProof(t, identity, operation, "password")
	wrong := securityEnrollmentRequest("PATCH", "/pat", `{"name":"wrong","scopes":["tokens:write"]}`, proof, identity, handler)
	require.Equal(t, http.StatusForbidden, wrong.Code)
	assert.Contains(t, wrong.Body.String(), "SECURITY_PROOF_CONTEXT_MISMATCH")
	response = securityEnrollmentRequest("PATCH", "/pat", `{"name":"updated","scopes":["profile:read","tokens:read","profile:read"]}`, proof, identity, handler)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	stored, _, err = model.ValidatePersonalAccessToken(raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"profile:read", "tokens:read"}, stored.Scopes)
	assert.Equal(t, "updated", stored.Name)
	assert.Equal(t, token.ExpiresAt, stored.ExpiresAt)
	response = securityEnrollmentRequest("PATCH", "/pat", `{"name":"replay","scopes":["tokens:read","profile:read"]}`, proof, identity, handler)
	assert.Contains(t, response.Body.String(), "SECURITY_PROOF_CONSUMED")

	proof = issueSecurityEnrollmentProof(t, identity, operation, "password")
	response = securityEnrollmentRequest("PATCH", "/pat", `{"scopes":["tokens:read","profile:read"]}`, proof, identity, handler)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	stored, err = model.GetActivePersonalAccessToken(identity.UserID, token.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated", stored.Name, "omitting name must not overwrite a concurrent rename")
}

func TestPATUpdateRejectsInvalidChangesWithoutMutatingToken(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	token, _, err := model.CreatePersonalAccessToken(identity, "original", []string{"profile:read"}, 0)
	require.NoError(t, err)
	handler := func(c *gin.Context) {
		c.Params = gin.Params{{Key: "id", Value: fmt.Sprint(token.ID)}}
		UpdatePersonalAccessToken(c)
	}
	for _, body := range []string{`{}`, `{"name":null}`, `{"name":"","scopes":["profile:read"]}`, `{"name":"bad","scopes":null}`, `{"name":"bad","scopes":[]}`, `{"name":"bad","scopes":["options:write"]}`, `{"name":"bad","expires_at":9999999999}`} {
		t.Run(body, func(t *testing.T) {
			response := securityEnrollmentRequest("PATCH", "/pat", body, "", identity, handler)
			assert.Equal(t, http.StatusBadRequest, response.Code)
			stored, err := model.GetActivePersonalAccessToken(identity.UserID, token.ID)
			require.NoError(t, err)
			assert.Equal(t, "original", stored.Name)
			assert.Equal(t, token.Scopes, stored.Scopes)
		})
	}
	response := securityEnrollmentRequest("PATCH", "/pat", `{"name":"bad","scopes":["tokens:read"]}`, "", identity, handler)
	assert.Equal(t, http.StatusForbidden, response.Code)
	require.NoError(t, model.DB.Model(token).Update("expires_at", time.Now().Unix()-1).Error)
	response = securityEnrollmentRequest("PATCH", "/pat", `{"name":"revive"}`, "", identity, handler)
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestPATUpdateRechecksOwnerAndSessionInTransaction(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	token, _, err := model.CreatePersonalAccessToken(identity, "original", []string{"profile:read"}, 0)
	require.NoError(t, err)
	other := model.User{Username: "pat-other", AffCode: "pat-other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&other).Error)
	foreign := model.PersonalAccessToken{UserID: other.Id, Name: "foreign", Digest: model.PersonalAccessTokenDigest("other"), Scopes: []string{"profile:read"}, ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, model.DB.Create(&foreign).Error)
	name := "bad"
	_, err = model.UpdatePersonalAccessToken(identity, foreign.ID, &name, nil, service.ValidatePATScopesWithTx)
	assert.ErrorIs(t, err, model.ErrPATNotFound)
	patIdentity := identity
	patIdentity.PersonalAccessTokenID = token.ID
	_, err = model.UpdatePersonalAccessToken(patIdentity, token.ID, &name, nil, service.ValidatePATScopesWithTx)
	assert.ErrorIs(t, err, model.ErrPATInvalid)
	_, err = model.RevokeUserSession(user.Id, identity.SessionID, "test-logout")
	require.NoError(t, err)
	_, err = model.UpdatePersonalAccessToken(identity, token.ID, &name, []string{"tokens:read"}, service.ValidatePATScopesWithTx)
	assert.ErrorIs(t, err, model.ErrUserSessionInactive)
	stored, err := model.GetActivePersonalAccessToken(user.Id, token.ID)
	require.NoError(t, err)
	assert.Equal(t, "original", stored.Name)
	assert.Equal(t, token.Scopes, stored.Scopes)
}

func TestPATScopeCatalogUsesCurrentOwnerPermissions(t *testing.T) {
	_, identity := setupSecurityEnrollmentTest(t)
	response := securityEnrollmentRequest("GET", "/pat/scopes", "", "", identity, GetPersonalAccessTokenScopes)
	require.Equal(t, http.StatusOK, response.Code)
	var payload struct {
		Data struct {
			Scopes              []string
			Limit               int
			DefaultLifetimeDays int `json:"default_lifetime_days"`
		}
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.Contains(t, payload.Data.Scopes, "tokens:read")
	assert.NotContains(t, payload.Data.Scopes, "options:read")
	assert.NotContains(t, payload.Data.Scopes, "channels:operate")
	assert.Equal(t, 20, payload.Data.Limit)
	assert.Equal(t, 30, payload.Data.DefaultLifetimeDays)
}
