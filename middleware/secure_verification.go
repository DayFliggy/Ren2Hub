package middleware

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// SecureVerificationRequired protects channel key disclosure. Other sensitive
// operations validate their narrower proof scopes in their controller.
func SecureVerificationRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		channelID, err := strconv.Atoi(c.Param("id"))
		if err != nil || channelID <= 0 {
			common.ApiErrorStatus(c, http.StatusBadRequest, service.ErrVerificationContextInvalid, gin.H{"code": "SECURITY_CONTEXT_INVALID"})
			c.Abort()
			return
		}
		context, err := common.Marshal(service.ChannelKeyReadContext{ChannelID: channelID})
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if RequireSecurityProof(c, service.VerificationOperation{Scope: service.VerificationScopeChannelKeyRead, Context: context}) == nil {
			return
		}
		c.Set("secure_verified", true)
		c.Next()
	}
}

// RequireSecurityProof validates a proof against the authenticated dashboard
// session and writes the shared proof error contract on failure.
func RequireSecurityProof(c *gin.Context, operation service.VerificationOperation) *model.AuthFlowAuthorization {
	identity, ok := GetAuthIdentity(c)
	if !ok {
		identity, ok = GetSessionAuthIdentity(c)
	}
	if !ok {
		securityProofError(c, "SECURITY_PROOF_INVALID", common.NewMessage("Security verification is no longer valid. Please verify again."))
		return nil
	}
	raw := strings.TrimSpace(c.GetHeader("X-Security-Proof"))
	if raw == "" {
		securityProofError(c, "SECURITY_PROOF_REQUIRED", common.NewMessage("Additional verification required"))
		return nil
	}
	authorization, err := service.ConsumeOperationProof(raw, identity, operation)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAuthTokenExpired):
			securityProofError(c, "SECURITY_PROOF_EXPIRED", common.NewMessage("Security verification has expired. Please verify again."))
		case errors.Is(err, service.ErrProofScope):
			securityProofError(c, "SECURITY_PROOF_SCOPE_MISMATCH", common.NewMessage("Verification does not match this action."))
		case errors.Is(err, service.ErrVerificationUnavailable):
			securityProofError(c, "SECURITY_METHOD_UNAVAILABLE", service.ErrVerificationUnavailable)
		case errors.Is(err, service.ErrProofMethod):
			securityProofError(c, "SECURITY_PROOF_METHOD_MISMATCH", common.NewMessage("This verification method is not allowed for this action."))
		case errors.Is(err, service.ErrProofConsumed):
			securityProofError(c, "SECURITY_PROOF_CONSUMED", common.NewMessage("This verification has already been used. Please verify again."))
		case errors.Is(err, service.ErrProofContext):
			securityProofError(c, "SECURITY_PROOF_CONTEXT_MISMATCH", common.NewMessage("Verification does not match this action's details. Please verify again."))
		case errors.Is(err, service.ErrVerificationForbidden):
			securityProofError(c, "SECURITY_ACTION_FORBIDDEN", service.ErrVerificationForbidden)
		case errors.Is(err, service.ErrAuthTokenInvalid), errors.Is(err, service.ErrLoginSessionInvalid), errors.Is(err, service.ErrLoginSessionRevoked), errors.Is(err, model.ErrUserSessionInactive):
			securityProofError(c, "SECURITY_PROOF_INVALID", common.NewMessage("Security verification is no longer valid. Please verify again."))
		default:
			_ = c.Error(err)
			common.ApiErrorStatus(c, http.StatusInternalServerError, common.NewMessage("Please try again later."), gin.H{"code": "AUTH_INTERNAL_ERROR"})
			c.Abort()
		}
		return nil
	}
	return authorization
}

// securityProofError answers a failed proof check with 403. A *common.Message
// message also gives the web console its message_key.
func securityProofError(c *gin.Context, code string, message error) {
	c.Set("security_error_code", code)
	common.ApiErrorStatus(c, http.StatusForbidden, message, gin.H{"code": code})
	c.Abort()
}
