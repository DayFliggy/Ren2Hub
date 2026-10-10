package service

import "time"

type AttemptOrigin string

type AttemptAcceptance string
type AttemptRetryPermission string

const (
	AttemptNotSent           AttemptAcceptance      = "not_sent"
	AttemptRejected          AttemptAcceptance      = "rejected"
	AttemptAccepted          AttemptAcceptance      = "accepted"
	AttemptAcceptanceUnknown AttemptAcceptance      = "unknown"
	AttemptRetryPolicy       AttemptRetryPermission = "policy"
	AttemptRetryForbidden    AttemptRetryPermission = "forbidden"
)

const (
	AttemptOriginProvider     AttemptOrigin = "provider"
	AttemptOriginTransport    AttemptOrigin = "transport"
	AttemptOriginLocalConvert AttemptOrigin = "local_convert"
	AttemptOriginLocalParse   AttemptOrigin = "local_parse"
	AttemptOriginAdmission    AttemptOrigin = "admission"
	AttemptOriginClientCancel AttemptOrigin = "client_cancel"
)

// AttemptOutcome carries attribution separately from whether replay is still safe.
// Message is only classified in memory and is never persisted in health rows.
type AttemptOutcome struct {
	Origin            AttemptOrigin
	Success           bool
	StatusCode        int
	ProviderCode      string
	ProviderType      string
	Message           string
	ResponseCommitted bool
	RetryAfter        time.Duration
	LatencyMS         int64
	TTFTMS            int64
	Acceptance        AttemptAcceptance
	RetryPermission   AttemptRetryPermission
	SideEffecting     bool
	UsageSource       string
}

func ClassifyAttemptOutcome(outcome AttemptOutcome) RouteErrorClassification {
	if outcome.Success {
		return RouteErrorClassification{}
	}
	var classification RouteErrorClassification
	switch outcome.Origin {
	case AttemptOriginProvider:
		classification = classifyProviderRouteError(outcome.StatusCode, outcome.ProviderCode, outcome.ProviderType, outcome.Message)
	case AttemptOriginTransport:
		classification = RouteErrorClassification{Class: RouteErrorTransient, Retryable: true, Failoverable: true, MarkChannelModel: true}
	case AttemptOriginAdmission:
		classification.Class = RouteErrorAdmission
	case AttemptOriginLocalConvert, AttemptOriginLocalParse:
		classification.Class = RouteErrorInput
	default:
		classification.Class = RouteErrorUnknown
	}
	if outcome.ResponseCommitted || outcome.RetryPermission == AttemptRetryForbidden ||
		(outcome.SideEffecting && (outcome.Acceptance == AttemptAcceptanceUnknown || outcome.Acceptance == AttemptAccepted)) {
		classification.Retryable, classification.Failoverable = false, false
	}
	return classification
}
