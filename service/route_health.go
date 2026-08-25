package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	return health.CooldownUntil > 0 && health.CooldownUntil <= now.Unix()
}

func LoadRouteHealth(ctx context.Context, channelID int, requestModel string) (model.ChannelHealth, error) {
	if model.DB == nil || channelID <= 0 {
		return model.ChannelHealth{}, errors.New("route health database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var health model.ChannelHealth
	err := model.DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, modellab.NormalizeModel(requestModel), "").First(&health).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		health = model.ChannelHealth{ChannelID: channelID, Model: modellab.NormalizeModel(requestModel), KeyScope: "", State: model.RouteHealthStateClosed, HealthEpoch: 1}
		return health, nil
	}
	return health, err
}

func RouteHealthUsable(ctx context.Context, channelID int, requestModel string, now time.Time) (bool, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < 2; attempt++ {
		health, err := LoadRouteHealth(ctx, channelID, requestModel)
		if err != nil {
			return false, 0, err
		}
		if health.State == "" || health.State == model.RouteHealthStateClosed {
			return true, health.HealthEpoch, nil
		}
		if health.State == model.RouteHealthStateHalfOpen {
			if health.CooldownUntil > now.Unix() {
				// A half-open row is owned by the request that atomically
				// transitioned it from open. Other requests wait for that
				// probe instead of creating an unbounded stampede.
				return false, health.HealthEpoch, nil
			}
			// A crashed or cancelled probe must not leave a route stuck in
			// half-open forever. Re-arm it as open before the next loop
			// iteration tries to claim a fresh probe.
			result := model.DB.WithContext(ctx).Model(&model.ChannelHealth{}).
				Where("id = ? AND state = ? AND cooldown_until > 0 AND cooldown_until <= ?", health.ID, model.RouteHealthStateHalfOpen, now.Unix()).
				Updates(map[string]any{
					"state":          model.RouteHealthStateOpen,
					"cooldown_until": 0,
					"health_epoch":   gorm.Expr("health_epoch + 1"),
					"updated_at":     now.Unix(),
				})
			if result.Error != nil {
				return false, health.HealthEpoch, result.Error
			}
			if result.RowsAffected != 1 {
				return false, health.HealthEpoch, nil
			}
			continue
		}
		if health.State != model.RouteHealthStateOpen || health.CooldownUntil <= 0 || health.CooldownUntil > now.Unix() {
			return false, health.HealthEpoch, nil
		}

		probeUntil := now.Add(DefaultRouteHealthPolicy().Cooldown).Unix()
		result := model.DB.WithContext(ctx).Model(&model.ChannelHealth{}).
			Where("id = ? AND state = ? AND cooldown_until > 0 AND cooldown_until <= ?", health.ID, model.RouteHealthStateOpen, now.Unix()).
			Updates(map[string]any{
				"state":          model.RouteHealthStateHalfOpen,
				"cooldown_until": probeUntil,
				"health_epoch":   gorm.Expr("health_epoch + 1"),
				"updated_at":     now.Unix(),
			})
		if result.Error != nil {
			return false, health.HealthEpoch, result.Error
		}
		if result.RowsAffected == 1 {
			return true, health.HealthEpoch + 1, nil
		}
		return false, health.HealthEpoch, nil
	}
	return false, 0, nil
}

func RouteHealthMetrics(ctx context.Context, channelID int, requestModel string) (errorRate, latencyMS float64, err error) {
	errorRate, latencyMS, _, err = RouteHealthMetricsWithTTFT(ctx, channelID, requestModel)
	return errorRate, latencyMS, err
}

func RouteHealthMetricsWithTTFT(ctx context.Context, channelID int, requestModel string) (errorRate, latencyMS, ttftMS float64, err error) {
	health, err := LoadRouteHealth(ctx, channelID, requestModel)
	if err != nil {
		return 0, 0, 0, err
	}
	policy := DefaultRouteHealthPolicy()
	errorRate = clamp01(float64(health.FailureCount) / float64(policy.FailureThreshold))
	latencyMS = float64(health.LastLatencyMS)
	ttftMS = float64(health.FirstTokenLatencyMS)
	return errorRate, latencyMS, ttftMS, nil
}

func PersistRouteHealthFailure(ctx context.Context, channelID int, requestModel string, policy RouteHealthPolicy, now time.Time) (model.ChannelHealth, error) {
	return persistRouteHealthObservation(ctx, channelID, requestModel, "", now, func(health model.ChannelHealth) model.ChannelHealth {
		return ObserveRouteHealthFailure(health, policy, now)
	})
}

func PersistRouteHealthSuccess(ctx context.Context, channelID int, requestModel string, now time.Time) (model.ChannelHealth, error) {
	return PersistRouteHealthSuccessWithMetrics(ctx, channelID, requestModel, now, 0, 0)
}

func PersistRouteHealthSuccessWithMetrics(ctx context.Context, channelID int, requestModel string, now time.Time, latencyMS, ttftMS int64) (model.ChannelHealth, error) {
	return persistRouteHealthObservation(ctx, channelID, requestModel, "", now, func(health model.ChannelHealth) model.ChannelHealth {
		if latencyMS >= 0 {
			health.LastLatencyMS = latencyMS
		}
		if ttftMS > 0 {
			health.FirstTokenLatencyMS = ttftMS
		}
		return ObserveRouteHealthSuccess(health, now)
	})
}

func ObserveLiveRouteError(ctx context.Context, channelID int, requestModel string, status int, providerCode, message string, streamStarted bool) error {
	return observeLiveRouteError(ctx, channelID, requestModel, "", status, providerCode, message, streamStarted)
}

func ObserveLiveRouteErrorForKey(ctx context.Context, channelID int, requestModel, key string, status int, providerCode, message string, streamStarted bool) error {
	return observeLiveRouteError(ctx, channelID, requestModel, RouteKeyScope(key), status, providerCode, message, streamStarted)
}

func observeLiveRouteError(ctx context.Context, channelID int, requestModel, keyScope string, status int, providerCode, message string, streamStarted bool) error {
	classification := ClassifyRouteError(status, providerCode, message, streamStarted)
	if !classification.MarkChannelModel && !classification.MarkCapability && !classification.MarkKey {
		return nil
	}
	if classification.MarkKey {
		if keyScope == "" {
			return nil
		}
		_, err := persistRouteHealthFailure(ctx, channelID, requestModel, keyScope, DefaultRouteHealthPolicy(), time.Now())
		return err
	}
	if !classification.MarkChannelModel {
		// Capability errors belong to the specific immutable capability and are
		// handled by the next capability refresh. They must not open the whole
		// channel/model circuit.
		return nil
	}
	_, err := PersistRouteHealthFailure(ctx, channelID, requestModel, DefaultRouteHealthPolicy(), time.Now())
	return err
}

func RouteKeyScope(key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("key:%x", sum[:])
}

func persistRouteHealthFailure(ctx context.Context, channelID int, requestModel, keyScope string, policy RouteHealthPolicy, now time.Time) (model.ChannelHealth, error) {
	return persistRouteHealthObservation(ctx, channelID, requestModel, keyScope, now, func(health model.ChannelHealth) model.ChannelHealth {
		return ObserveRouteHealthFailure(health, policy, now)
	})
}

func persistRouteHealthObservation(ctx context.Context, channelID int, requestModel, keyScope string, now time.Time, observe func(model.ChannelHealth) model.ChannelHealth) (model.ChannelHealth, error) {
	if model.DB == nil || channelID <= 0 || observe == nil {
		return model.ChannelHealth{}, errors.New("route health database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	modelName := modellab.NormalizeModel(requestModel)
	var health model.ChannelHealth
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, modelName, keyScope).
			First(&health).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			health = model.ChannelHealth{ChannelID: channelID, Model: modelName, KeyScope: keyScope}
		} else if err != nil {
			return err
		}
		health = observe(health)
		if health.ID == 0 {
			return tx.Create(&health).Error
		}
		return tx.Save(&health).Error
	})
	return health, err
}

func ObserveLiveRouteSuccess(ctx context.Context, channelID int, requestModel string) error {
	_, err := PersistRouteHealthSuccess(ctx, channelID, requestModel, time.Now())
	return err
}

func ObserveLiveRouteSuccessWithMetrics(ctx context.Context, channelID int, requestModel string, latencyMS, ttftMS int64) error {
	_, err := PersistRouteHealthSuccessWithMetrics(ctx, channelID, requestModel, time.Now(), latencyMS, ttftMS)
	return err
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
	// Retry-After is the provider's minimum wait. Local full jitter may make
	// the request wait longer, but it must never shorten that provider bound.
	if retryAfter > delay {
		delay = retryAfter
	}
	if remaining > 0 && delay > remaining {
		return remaining
	}
	return delay
}

// ParseRetryAfter accepts the RFC 9110 delta-seconds and HTTP-date forms.
// Invalid or negative values are ignored; the caller applies the remaining
// request deadline as the final upper bound.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if when.Before(now) {
		return 0, true
	}
	return when.Sub(now), true
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
