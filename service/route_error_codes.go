package service

import (
	"strings"

	"github.com/QuantumNous/new-api/relaykit/types"
)

// routeErrorClass groups the error codes that routing reads.
type routeErrorClass int

const (
	// routeErrorLocal: a code this gateway generates.
	routeErrorLocal routeErrorClass = iota + 1
	// routeErrorAccount: credential, quota, rate limit or permission.
	routeErrorAccount
	// routeErrorServer: the upstream's own failure.
	routeErrorServer
	// routeErrorRequest: the request's fault.
	routeErrorRequest
)

// nativeRouteErrorCodes are the codes and types the native classifier
// (classifyFailure in pkg/perf_metrics/outcome.go) names, with the class that
// classifier gives them; account and server split the native failures. That
// file is upstream text and does not export its list, so the names appear
// here once: TestRouteErrorCodesListEveryNativeName compares them with the
// native source and TestRouteErrorCodesAgreeWithNativeClassifier checks every
// class against the native verdict.
var nativeRouteErrorCodes = map[string]routeErrorClass{
	"invalid_api_key": routeErrorAccount, "api_key_invalid": routeErrorAccount, "api_key_expired": routeErrorAccount,
	"api_key_service_blocked": routeErrorAccount, "invalid_authentication": routeErrorAccount, "authentication_error": routeErrorAccount,
	"unauthenticated": routeErrorAccount, "permission_denied": routeErrorAccount, "permission_error": routeErrorAccount,
	"access_denied": routeErrorAccount, "insufficient_quota": routeErrorAccount, "quota_exceeded": routeErrorAccount,
	"resource_exhausted": routeErrorAccount, "rate_limit_exceeded": routeErrorAccount, "rate_limit_error": routeErrorAccount,
	"insufficient_user_quota": routeErrorAccount, "pre_consume_token_quota_failed": routeErrorAccount,
	"overloaded_error": routeErrorServer, "server_error": routeErrorServer, "internal_error": routeErrorServer,
	"service_unavailable": routeErrorServer, "model_not_found": routeErrorServer,
	"context_length_exceeded": routeErrorRequest, "invalid_request": routeErrorRequest, "invalid_request_error": routeErrorRequest,
	"invalid_argument": routeErrorRequest, "sensitive_words_detected": routeErrorRequest, "prompt_blocked": routeErrorRequest,
	"content_filter": routeErrorRequest, "content_policy_violation": routeErrorRequest, "safety": routeErrorRequest,
}

// localRouteErrorCodes are the codes this gateway generates: the relaykit
// error codes and the routing reasons.
var localRouteErrorCodes = []string{
	string(types.ErrorCodeInvalidRequest), string(types.ErrorCodeSensitiveWordsDetected), string(types.ErrorCodeRequestRejected),
	string(types.ErrorCodeRequestPluginFailed), string(types.ErrorCodeResponseRejected), string(types.ErrorCodeViolationFeeGrokCSAM),
	string(types.ErrorCodeCountTokenFailed), string(types.ErrorCodeModelPriceError), string(types.ErrorCodeInvalidApiType),
	string(types.ErrorCodeJsonMarshalFailed), string(types.ErrorCodeDoRequestFailed), string(types.ErrorCodeGetChannelFailed),
	string(types.ErrorCodeGenRelayInfoFailed), string(types.ErrorCodeChannelNoAvailableKey), string(types.ErrorCodeChannelParamOverrideInvalid),
	string(types.ErrorCodeChannelHeaderOverrideInvalid), string(types.ErrorCodeChannelModelMappedError), string(types.ErrorCodeChannelAwsClientError),
	string(types.ErrorCodeChannelInvalidKey), string(types.ErrorCodeChannelResponseTimeExceeded), string(types.ErrorCodeReadRequestBodyFailed),
	string(types.ErrorCodeConvertRequestFailed), string(types.ErrorCodeAccessDenied), string(types.ErrorCodeBadRequestBody),
	string(types.ErrorCodeReadResponseBodyFailed), string(types.ErrorCodeBadResponseStatusCode), string(types.ErrorCodeBadResponse),
	string(types.ErrorCodeBadResponseBody), string(types.ErrorCodeEmptyResponse), string(types.ErrorCodeAwsInvokeError),
	string(types.ErrorCodeModelNotFound), string(types.ErrorCodePromptBlocked), string(types.ErrorCodeQueryDataError),
	string(types.ErrorCodeUpdateDataError), string(types.ErrorCodeInsufficientUserQuota), string(types.ErrorCodePreConsumeTokenQuotaFailed),
	routeReasonContextOverflow, routeReasonEmptyStream, routeReasonNoVisibleOutput, routeReasonRequestTerminate, routeReasonNoGeneration,
}

// routeErrorCodes is the one table of error codes routing reads: the account
// signals that message text never overrules (requestContextOverflow) and the
// codes the breaker snapshot may show (routeErrorCode). A local code that the
// native classifier also names keeps the native class.
var routeErrorCodes = func() map[string]routeErrorClass {
	codes := make(map[string]routeErrorClass, len(nativeRouteErrorCodes)+len(localRouteErrorCodes))
	for code, class := range nativeRouteErrorCodes {
		codes[code] = class
	}
	for _, code := range localRouteErrorCodes {
		if _, ok := codes[code]; !ok {
			codes[code] = routeErrorLocal
		}
	}
	return codes
}()

// routeUnrecognizedErrorCode replaces an error code outside routeErrorCodes:
// upstreams fill code freely, sometimes with a sentence, an object, an
// identifier or a credential. The client still receives the original error.
const routeUnrecognizedErrorCode = "unrecognized"

func routeErrorCodeClass(code string) (routeErrorClass, bool) {
	class, ok := routeErrorCodes[strings.ToLower(code)]
	return class, ok
}

// routeErrorCode returns the code the breaker snapshot may store.
func routeErrorCode(code string) string {
	if code == "" {
		return ""
	}
	if _, ok := routeErrorCodeClass(code); !ok {
		return routeUnrecognizedErrorCode
	}
	return strings.ToLower(code)
}
