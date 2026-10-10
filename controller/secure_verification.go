package controller

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/oauth"
	"github.com/QuantumNous/new-api/service"
	passkeysvc "github.com/QuantumNous/new-api/service/passkey"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
)

func GetVerificationMethods(c *gin.Context) {
	identity, ok := middleware.GetAuthIdentity(c)
	if !ok {
		identity, ok = middleware.GetSessionAuthIdentity(c)
	}
	if !ok {
		common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("The current sign-in method does not support security verification"))
		return
	}
	requirements, err := service.GetVerificationRequirements(identity, c.Query("scope"))
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	common.ApiSuccess(c, requirements)
}

// writeSecurityOperationError only exposes known, fixed business messages.
// Unexpected errors retain their cause for the existing server-side auth logger.
func writeSecurityOperationError(c *gin.Context, err error) {
	status := http.StatusOK
	var code string
	// message is the error the client sees. A *common.Message, given here or
	// wrapped in err, also gives the web console its message_key.
	var message error
	var protocolError *protocol.Error
	switch {
	case errors.Is(err, model.ErrPATInput):
		status, code, message = http.StatusBadRequest, "PAT_INPUT_INVALID", err
	case errors.Is(err, model.ErrPATLimit):
		status, code, message = http.StatusTooManyRequests, "PAT_LIMIT_REACHED", err
	case errors.Is(err, model.ErrPATNotFound):
		status, code, message = http.StatusNotFound, "PAT_NOT_FOUND", err
	case errors.Is(err, model.ErrPATInvalid):
		status, code, message = http.StatusForbidden, "PAT_INVALID", err
	case errors.Is(err, passkeysvc.ErrRPIDUnavailable):
		code, message = "PASSKEY_RP_ID_UNAVAILABLE", common.NewMessage("This Passkey domain is not available on this website. Use its original website or another verification method.")
	case errors.Is(err, system_setting.ErrPasskeyRPIDInvalid):
		code, message = "PASSKEY_RP_ID_INVALID", common.NewMessage("Invalid Passkey domain. Enter a domain without a scheme, port, path or wildcard.")
	case errors.Is(err, service.ErrAccountEmailInvalid), errors.Is(err, service.ErrAccountEmailRestricted):
		code, message = "EMAIL_ADDRESS_REJECTED", err
	case errors.Is(err, model.ErrEmailAlreadyTaken):
		code, message = "EMAIL_ALREADY_TAKEN", common.NewMessage("This email address is already in use.")
	case errors.Is(err, service.ErrEmailBindingDelivery):
		code, message = "EMAIL_BINDING_DELIVERY_FAILED", err
	case errors.Is(err, model.ErrEmailBindingCodeInvalid):
		code, message = "EMAIL_BINDING_CODE_INVALID", err
	case errors.Is(err, model.ErrEmailBindingLocked):
		code, message = "EMAIL_BINDING_LOCKED", err
	case errors.Is(err, model.ErrEmailBindingResendWait):
		status = http.StatusTooManyRequests
		code, message = "EMAIL_BINDING_RESEND_WAIT", err
	case errors.Is(err, common.ErrAccountPasswordLength), errors.Is(err, common.ErrAccountPasswordSame), errors.Is(err, common.ErrPasswordLegacyLimit):
		code, message = "PASSWORD_POLICY_REJECTED", err
	case errors.Is(err, model.ErrCurrentPasswordInvalid):
		code, message = "CURRENT_PASSWORD_INVALID", err
	case errors.Is(err, model.ErrAccountPasswordState), errors.Is(err, model.ErrAccountBindingChanged):
		status = http.StatusConflict
		code, message = "ACCOUNT_SECURITY_STATE_CHANGED", err
	case errors.Is(err, model.ErrLastLoginMethod):
		code, message = "LAST_LOGIN_METHOD", err
	case errors.Is(err, oauth.ErrTelegramOAuthNotConfigured):
		code, message = "TELEGRAM_OAUTH_NOT_CONFIGURED", oauth.ErrTelegramOAuthNotConfigured
	case errors.Is(err, oauth.ErrTelegramOAuthConflict):
		code, message = "TELEGRAM_OAUTH_CONFLICT", oauth.ErrTelegramOAuthConflict
	case errors.Is(err, oauth.ErrTelegramOAuthFailed):
		code, message = "TELEGRAM_OAUTH_FAILED", oauth.ErrTelegramOAuthFailed
	case errors.Is(err, oauth.ErrTelegramAccountNotBound):
		code, message = "TELEGRAM_ACCOUNT_NOT_BOUND", oauth.ErrTelegramAccountNotBound
	case errors.Is(err, model.ErrExternalIdentityAlreadyClaimed):
		code, message = "ACCOUNT_ALREADY_BOUND", common.NewMessage("This external account is already bound.")
		if c.Param("provider") == "telegram" {
			code, message = "TELEGRAM_BIND_ALREADY_BOUND", common.NewMessage("This Telegram account is already bound.")
		}
	case errors.Is(err, service.ErrVerificationContextInvalid):
		status = http.StatusBadRequest
		code, message = "SECURITY_CONTEXT_INVALID", service.ErrVerificationContextInvalid
	case errors.Is(err, service.ErrVerificationForbidden):
		status = http.StatusForbidden
		code, message = "SECURITY_ACTION_FORBIDDEN", service.ErrVerificationForbidden
	case errors.Is(err, service.ErrVerificationFailed), errors.As(err, &protocolError):
		code, message = "SECURITY_VERIFICATION_FAILED", service.ErrVerificationFailed
	case errors.Is(err, service.ErrVerificationLocked):
		code, message = "SECURITY_VERIFICATION_LOCKED", service.ErrVerificationLocked
	case errors.Is(err, service.ErrVerificationUnavailable):
		code, message = "SECURITY_METHOD_UNAVAILABLE", service.ErrVerificationUnavailable
	case errors.Is(err, service.ErrVerificationFlowRequired):
		status = http.StatusBadRequest
		code, message = "SECURITY_VERIFICATION_FLOW_REQUIRED", service.ErrVerificationFlowRequired
	case errors.Is(err, service.ErrProofMethod):
		code, message = "SECURITY_PROOF_METHOD_MISMATCH", common.NewMessage("This verification method is not allowed for this action.")
	case errors.Is(err, service.ErrProofScope):
		code, message = "SECURITY_PROOF_SCOPE_MISMATCH", common.NewMessage("Verification does not match this action.")
	case errors.Is(err, service.ErrOAuthAccountMismatch):
		code, message = "OAUTH_ACCOUNT_MISMATCH", service.ErrOAuthAccountMismatch
	case errors.Is(err, model.ErrTwoFASetupInvalid):
		status = http.StatusConflict
		code, message = "TWOFA_SETUP_INVALID", model.ErrTwoFASetupInvalid
	case errors.Is(err, model.ErrTwoFACodeInvalid):
		code, message = "TWOFA_CODE_INVALID", model.ErrTwoFACodeInvalid
	case errors.Is(err, model.ErrTwoFAAlreadyEnabled):
		code, message = "TWOFA_ALREADY_ENABLED", common.NewMessage("Two-factor authentication is already enabled.")
	case errors.Is(err, model.ErrTwoFANotEnabled):
		code, message = "TWOFA_NOT_ENABLED", common.NewMessage("Two-factor authentication is not enabled.")
	case errors.Is(err, model.ErrPasskeyNotFound):
		code, message = "PASSKEY_NOT_FOUND", common.NewMessage("No Passkey is registered.")
	case errors.Is(err, model.ErrAuthFlowInvalid), errors.Is(err, model.ErrAuthFlowExpired), errors.Is(err, model.ErrAuthFlowConsumed):
		code, message = "AUTH_FLOW_INVALID", common.NewMessage("Verification flow expired")
	case errors.Is(err, model.ErrUserSessionInvalid), errors.Is(err, model.ErrUserSessionInactive):
		writeAuthSessionError(c, service.ErrAuthTokenInvalid)
		return
	default:
		c.Set("security_error_code", "AUTH_INTERNAL_ERROR")
		writeAuthSessionError(c, err)
		return
	}
	c.Set("security_error_code", code)
	if strings.Contains(c.Request.URL.Path, "/passkey/") {
		reason := "verification_failed"
		if errors.As(err, &protocolError) && slices.Contains([]string{"invalid_request", "challenge_mismatch", "parse_error", "auth_data", "verification_error", "invalid_signature", "invalid_key_type", "unsupported_key_algorithm"}, protocolError.Type) {
			reason = protocolError.Type
		}
		// Protocol details can contain challenges and client-controlled data.
		// Only fixed categories and the server-selected public RP ID are logged.
		logger.LogWarn(c.Request.Context(), common.LogText("passkey verification rejected: code=%s reason=%s rp_id=%q", code, reason, c.GetString("passkey_rp_id")))
	}
	common.ApiErrorStatus(c, status, message, gin.H{"code": code})
}

func UniversalVerify(c *gin.Context) {
	identity, ok := middleware.GetAuthIdentity(c)
	if !ok {
		identity, ok = middleware.GetSessionAuthIdentity(c)
	}
	if !ok {
		common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("The current sign-in method does not support security verification"))
		return
	}
	var request service.VerificationInput
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	proof, err := service.VerifySecurityInput(identity, request)
	if err != nil {
		writeSecurityOperationError(c, err)
		return
	}
	recordUserSecurityAudit(c, identity.UserID, "user.security_verify", map[string]any{"method": proof.Method, "scope": proof.Scope})
	common.ApiSuccess(c, proof)
}
