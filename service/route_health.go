package service

import (
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// RouteErrorClass is deliberately independent from provider-specific error
// types. The relay adapter can map its error into this small vocabulary before
// health and retry policy are consulted.
type RouteErrorClass string

const (
	RouteErrorInput         RouteErrorClass = "input"
	RouteErrorPermission    RouteErrorClass = "permission"
	RouteErrorKey           RouteErrorClass = "key"
	RouteErrorTransient     RouteErrorClass = "provider_transient"
	RouteErrorModel         RouteErrorClass = "model_capability"
	RouteErrorStreamStarted RouteErrorClass = "stream_started"
	RouteErrorUnknown       RouteErrorClass = "unknown"
)

type RouteErrorClassification struct {
	Class            RouteErrorClass
	Retryable        bool
	Failoverable     bool
	MarkKey          bool
	MarkCapability   bool
	MarkChannelModel bool
}

// ClassifyRouteError keeps retry and health side effects out of HTTP error
// parsing. Status, provider code, and a short message are the only inputs; no
// request body or credential is ever retained.
func ClassifyRouteError(status int, providerCode, message string, streamStarted bool) RouteErrorClassification {
	if streamStarted {
		return RouteErrorClassification{Class: RouteErrorStreamStarted}
	}
	code := strings.ToLower(strings.TrimSpace(providerCode))
	text := strings.ToLower(strings.TrimSpace(message))
	switch {
	case strings.Contains(code, "invalid_api_key") || strings.Contains(code, "invalid_credential") ||
		strings.Contains(code, "authentication_error") || strings.Contains(text, "invalid api key") ||
		strings.Contains(text, "incorrect api key"):
		return RouteErrorClassification{Class: RouteErrorKey, MarkKey: true}
	case status == 400 || status == 422 || strings.Contains(code, "invalid_request"):
		return RouteErrorClassification{Class: RouteErrorInput}
	case status == 401:
		return RouteErrorClassification{Class: RouteErrorKey, MarkKey: true}
	case status == 403 || strings.Contains(code, "permission"):
		return RouteErrorClassification{Class: RouteErrorPermission}
	case status == 404 || strings.Contains(code, "model_not_found") || strings.Contains(code, "unsupported_model") || strings.Contains(text, "model not found"):
		return RouteErrorClassification{Class: RouteErrorModel, MarkCapability: true}
	case status == 408 || status == 409 || status == 425 || status == 429 || status >= 500 || strings.Contains(code, "rate_limit") || strings.Contains(code, "timeout"):
		return RouteErrorClassification{Class: RouteErrorTransient, Retryable: true, Failoverable: true, MarkChannelModel: true}
	default:
		return RouteErrorClassification{Class: RouteErrorUnknown, Retryable: status >= 500, Failoverable: status >= 500, MarkChannelModel: status >= 500}
	}
}

// CanRouteFailover is the final guard before a retry may switch resources.
// Once a stream has started producing valid output, the caller must not
// splice another provider response onto it.
func CanRouteFailover(class RouteErrorClassification, streamStarted, hasValidOutput bool) bool {
	if streamStarted || hasValidOutput {
		return false
	}
	return class.Failoverable
}

type RouteRetryBudget struct {
	SameKeyAttempts     int
	SameChannelAttempts int
	FailoverAttempts    int
	TotalAttempts       int
}

const (
	DefaultSameKeyAttempts     = 0
	DefaultSameChannelAttempts = 1
	DefaultFailoverAttempts    = 2
	DefaultTotalAttempts       = 3
	DefaultRetryBackoff        = 250 * time.Millisecond
	DefaultRetryBackoffMax     = 2 * time.Second
)

func DefaultRouteRetryBudget() RouteRetryBudget {
	return RouteRetryBudget{
		SameKeyAttempts:     DefaultSameKeyAttempts,
		SameChannelAttempts: DefaultSameChannelAttempts,
		FailoverAttempts:    DefaultFailoverAttempts,
		TotalAttempts:       DefaultTotalAttempts,
	}
}

func (b RouteRetryBudget) Allows(class RouteErrorClassification, sameKey, sameChannel bool, attempt int) bool {
	if !class.Retryable || attempt >= b.TotalAttempts {
		return false
	}
	if sameKey {
		return attempt < b.SameKeyAttempts
	}
	if sameChannel {
		return attempt < b.SameChannelAttempts
	}
	return attempt < b.FailoverAttempts
}

type RouteHealthPolicy struct {
	FailureThreshold int
	Cooldown         time.Duration
}

func DefaultRouteHealthPolicy() RouteHealthPolicy {
	return RouteHealthPolicy{FailureThreshold: 3, Cooldown: 30 * time.Second}
}

func (p RouteHealthPolicy) normalized() RouteHealthPolicy {
	if p.FailureThreshold <= 0 {
		p.FailureThreshold = 3
	}
	if p.Cooldown <= 0 {
		p.Cooldown = 30 * time.Second
	}
	return p
}

func CanUseRouteHealth(health model.ChannelHealth, now time.Time) bool {
	if health.State == "" || health.State == model.RouteHealthStateClosed {
		return true
	}
	if health.State == model.RouteHealthStateHalfOpen {
		return true
	}
	return false
}

// RouteBackoff returns full-jitter delay for the local exponential backoff.
// Retry-After is a provider lower bound; both are clipped by the remaining
// request deadline.
func RouteBackoff(attempt int, retryAfter, remaining time.Duration, random01 float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	base := DefaultRetryBackoff * time.Duration(1<<routeMinInt(attempt, 4))
	if base > DefaultRetryBackoffMax {
		base = DefaultRetryBackoffMax
	}
	if random01 < 0 {
		random01 = 0
	}
	if random01 > 1 {
		random01 = 1
	}
	delay := time.Duration(float64(base) * random01)
	if retryAfter > 0 {
		delay += retryAfter
	}
	if remaining > 0 && delay > remaining {
		return remaining
	}
	return delay
}

func ObserveRouteHealthFailure(health model.ChannelHealth, policy RouteHealthPolicy, now time.Time) model.ChannelHealth {
	policy = policy.normalized()
	health.Normalize(now)
	health.FailureCount++
	if health.State == model.RouteHealthStateHalfOpen || health.FailureCount >= policy.FailureThreshold {
		health.State = model.RouteHealthStateOpen
		health.CooldownUntil = now.Add(policy.Cooldown).Unix()
		health.HealthEpoch++
	}
	health.UpdatedAt = now.Unix()
	return health
}

func ObserveRouteHealthSuccess(health model.ChannelHealth, now time.Time) model.ChannelHealth {
	health.Normalize(now)
	stateChanged := health.State != model.RouteHealthStateClosed || health.FailureCount != 0 || health.CooldownUntil != 0
	health.State = model.RouteHealthStateClosed
	health.FailureCount = 0
	health.CooldownUntil = 0
	if stateChanged {
		health.HealthEpoch++
	}
	health.UpdatedAt = now.Unix()
	return health
}

func EnterRouteHealthHalfOpen(health model.ChannelHealth, now time.Time) model.ChannelHealth {
	health.Normalize(now)
	if health.State == model.RouteHealthStateOpen && health.CooldownUntil <= now.Unix() {
		health.State = model.RouteHealthStateHalfOpen
		health.HealthEpoch++
		health.UpdatedAt = now.Unix()
	}
	return health
}

func routeMinInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
