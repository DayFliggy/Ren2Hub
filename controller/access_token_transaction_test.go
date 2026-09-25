package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccessTokenBusinessWriteRechecksSessionAfterProofConsumption(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		t.Run(map[bool]string{false: "generate", true: "revoke"}[revoke], func(t *testing.T) {
			user, identity := setupSecurityEnrollmentTest(t)
			old := "old-personal-access-token-123456"
			require.NoError(t, model.UpdateUserAccessToken(user.Id, old))
			scope := service.VerificationScopeAccessTokenGenerate
			if revoke {
				scope = service.VerificationScopeAccessTokenRevoke
			}
			operation := service.VerificationOperation{Scope: scope}
			proof := issueSecurityEnrollmentProof(t, identity, operation, service.VerificationMethodPassword)
			authorization, err := service.ConsumeOperationProof(proof, identity, operation)
			require.NoError(t, err)
			// The channel is a deterministic barrier between authorization and
			// the business write, matching a concurrent logout completing here.
			revoked := make(chan error, 1)
			go func() { _, err := model.RevokeUserSession(user.Id, identity.SessionID, "test_logout"); revoked <- err }()
			require.NoError(t, <-revoked)
			next := "new-personal-access-token-123456"
			var token *string
			if !revoke {
				token = &next
			}
			_, err = model.ChangeAccessTokenForSession(authorization.AuthSessionIdentity, token)
			require.ErrorIs(t, err, model.ErrUserSessionInactive)
			stored, err := model.GetUserById(user.Id, true)
			require.NoError(t, err)
			assert.Equal(t, old, stored.GetAccessToken())
			var flow model.AuthFlow
			require.NoError(t, model.DB.First(&flow, authorization.ProofID).Error)
			assert.NotNil(t, flow.ConsumedAt, "failed business writes must not restore the proof")
		})
	}
}

func TestAccessTokenBusinessWriteRejectsChangedUserAuthVersion(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	operation := service.VerificationOperation{Scope: service.VerificationScopeAccessTokenGenerate}
	proof := issueSecurityEnrollmentProof(t, identity, operation, service.VerificationMethodPassword)
	authorization, err := service.ConsumeOperationProof(proof, identity, operation)
	require.NoError(t, err)
	require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", user.Id).Update("auth_version", identity.UserAuthVersion+1).Error)
	token := "new-personal-access-token-123456"
	_, err = model.ChangeAccessTokenForSession(authorization.AuthSessionIdentity, &token)
	require.ErrorIs(t, err, model.ErrUserSessionInactive)
	status, err := model.GetUserAccessTokenStatus(user.Id)
	require.NoError(t, err)
	assert.False(t, status.Exists)
}
