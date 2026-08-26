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
	RouteErrorAdmission     RouteErrorClass = "route_admission"
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
	case strings.Contains(code, "route_lease") || strings.Contains(code, "route_admission") || strings.Contains(text, "route lease"):
		return RouteErrorClassification{Class: RouteErrorAdmission}
	case strings.Contains(code, "invalid_api_key") || strings.Contains(code, "invalid_credential") ||
		strings.Contains(code, "authentication_error") || strings.Contains(text, "invalid api key") ||
		strings.Contains(text, "incorrect api key"):
		return RouteErrorClassification{Class: RouteErrorKey, Retryable: true, Failoverable: true, MarkKey: true}
	case strings.Contains(code, "model_not_found") || strings.Contains(code, "unsupported_model") || strings.Contains(code, "model_not_supported") || strings.Contains(code, "invalid_model") || strings.Contains(text, "model not found") || strings.Contains(text, "unsupported model") || strings.Contains(text, "invalid model"):
		return RouteErrorClassification{Class: RouteErrorModel, Failoverable: true, MarkCapability: true}
	case status == 400 || status == 422 || strings.Contains(code, "invalid_request"):
		return RouteErrorClassification{Class: RouteErrorInput}
	case status == 401:
		return RouteErrorClassification{Class: RouteErrorKey, Retryable: true, Failoverable: true, MarkKey: true}
	case status == 403 || strings.Contains(code, "permission"):
		return RouteErrorClassification{Class: RouteErrorPermission}
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

type RouteRetryCounters struct {
	SameKeyAttempts     int
	SameChannelAttempts int
	FailoverAttempts    int
	TotalAttempts       int
}

type RouteRetryRelation string

const (
	RouteRetrySameKey     RouteRetryRelation = "same_key"
	RouteRetrySameChannel RouteRetryRelation = "same_channel"
	RouteRetryFailover    RouteRetryRelation = "failover"
)

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

func (b RouteRetryBudget) Allows(class RouteErrorClassification, relation RouteRetryRelation, counters RouteRetryCounters) bool {
	if counters.TotalAttempts >= b.TotalAttempts {
		return false
	}
	switch relation {
	case RouteRetrySameKey:
		return class.Retryable && counters.SameKeyAttempts < b.SameKeyAttempts
	case RouteRetrySameChannel:
		return class.Retryable && counters.SameChannelAttempts < b.SameChannelAttempts
	case RouteRetryFailover:
		return class.Failoverable && counters.FailoverAttempts < b.FailoverAttempts
	default:
		return false
	}
}

type RouteHealthPolicy struct {
	FailureThreshold int
	Cooldown         time.Duration
}

func DefaultRouteHealthPolicy() RouteHealthPolicy {
	return RouteHealthPolicy{FailureThreshold: 3, Cooldown: 30 * time.Second}
}

func routeKeyHealthPolicy() RouteHealthPolicy {
	return RouteHealthPolicy{FailureThreshold: 1, Cooldown: 5 * time.Minute}
}

func routeCapabilityHealthPolicy() RouteHealthPolicy {
	return RouteHealthPolicy{FailureThreshold: 1, Cooldown: 5 * time.Minute}
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
		// A half-open route is only admitted by RouteHealthUsable, which
		// atomically claims the single live probe. Pure Shadow/score reads must
		// not make every concurrent request look usable.
		return false
	}
	return health.CooldownUntil > 0 && health.CooldownUntil <= now.Unix()
}

func LoadRouteHealth(ctx context.Context, channelID int, requestModel string) (model.ChannelHealth, error) {
	return loadRouteHealthScope(ctx, channelID, requestModel, "")
}

func loadRouteHealthScope(ctx context.Context, channelID int, requestModel, keyScope string) (model.ChannelHealth, error) {
	if model.DB == nil || channelID <= 0 {
		return model.ChannelHealth{}, errors.New("route health database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var health model.ChannelHealth
	err := model.DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, modellab.NormalizeModel(requestModel), keyScope).First(&health).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		health = model.ChannelHealth{ChannelID: channelID, Model: modellab.NormalizeModel(requestModel), KeyScope: keyScope, State: model.RouteHealthStateClosed, HealthEpoch: 1}
		return health, nil
	}
	return health, err
}

func RouteHealthUsable(ctx context.Context, channelID int, requestModel string, now time.Time) (bool, int64, error) {
	return routeHealthScopeUsable(ctx, channelID, requestModel, "", now)
}

// RouteKeyHealthUsable atomically admits a recovered key's single half-open
// probe. The stored scope is a hash, so this boundary never persists or logs
// the credential itself.
func RouteKeyHealthUsable(ctx context.Context, channelID int, requestModel, key string, now time.Time) (bool, int64, error) {
	if strings.TrimSpace(key) == "" {
		return false, 0, nil
	}
	return routeHealthScopeUsable(ctx, channelID, requestModel, RouteKeyScope(key), now)
}

func routeHealthScopeUsable(ctx context.Context, channelID int, requestModel, keyScope string, now time.Time) (bool, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; attempt < 2; attempt++ {
		health, err := loadRouteHealthScope(ctx, channelID, requestModel, keyScope)
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
	metrics, err := RouteHealthScoringMetrics(ctx, channelID, requestModel)
	if err != nil {
		return 0, 0, 0, err
	}
	return metrics.ErrorRate, metrics.LatencyMS, metrics.TTFTMS, nil
}

type RouteHealthScoreMetrics struct {
	ErrorRate      float64
	ErrorRateKnown bool
	LatencyMS      float64
	LatencyKnown   bool
	TTFTMS         float64
	TTFTKnown      bool
}

// RouteHealthScoringMetrics keeps absence distinct from a measured perfect
// value. A missing health row is neutral input for scoring, not zero latency
// or a proven zero error rate.
func RouteHealthScoringMetrics(ctx context.Context, channelID int, requestModel string) (RouteHealthScoreMetrics, error) {
	health, err := LoadRouteHealth(ctx, channelID, requestModel)
	if err != nil {
		return RouteHealthScoreMetrics{}, err
	}
	return routeHealthScoreMetrics(health), nil
}

func routeHealthScoreMetrics(health model.ChannelHealth) RouteHealthScoreMetrics {
	if health.ID == 0 {
		return RouteHealthScoreMetrics{}
	}
	policy := DefaultRouteHealthPolicy()
	return RouteHealthScoreMetrics{
		ErrorRate:      clamp01(float64(health.FailureCount) / float64(policy.FailureThreshold)),
		ErrorRateKnown: true,
		LatencyMS:      float64(health.LastLatencyMS),
		LatencyKnown:   health.LastLatencyMS > 0,
		TTFTMS:         float64(health.FirstTokenLatencyMS),
		TTFTKnown:      health.FirstTokenLatencyMS > 0,
	}
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
	return observeLiveRouteError(ctx, channelID, requestModel, "", status, providerCode, message, streamStarted, 0)
}

func ObserveLiveRouteErrorForKey(ctx context.Context, channelID int, requestModel, key string, status int, providerCode, message string, streamStarted bool) error {
	return ObserveLiveRouteErrorForKeyWithRetryAfter(ctx, channelID, requestModel, key, status, providerCode, message, streamStarted, 0)
}

func ObserveLiveRouteErrorForKeyWithRetryAfter(ctx context.Context, channelID int, requestModel, key string, status int, providerCode, message string, streamStarted bool, retryAfter time.Duration) error {
	keyScope := ""
	if strings.TrimSpace(key) != "" {
		keyScope = RouteKeyScope(key)
	}
	return observeLiveRouteError(ctx, channelID, requestModel, keyScope, status, providerCode, message, streamStarted, retryAfter)
}

func observeLiveRouteError(ctx context.Context, channelID int, requestModel, keyScope string, status int, providerCode, message string, streamStarted bool, retryAfter time.Duration) error {
	classification := ClassifyRouteError(status, providerCode, message, streamStarted)
	if !classification.MarkChannelModel && !classification.MarkCapability && !classification.MarkKey {
		return nil
	}
	if classification.MarkKey {
		if keyScope == "" {
			return nil
		}
		_, err := persistRouteHealthFailureWithRetryAfter(ctx, channelID, requestModel, keyScope, routeKeyHealthPolicy(), time.Now(), 0)
		return err
	}
	if classification.MarkCapability {
		_, err := persistRouteHealthFailureWithRetryAfter(ctx, channelID, requestModel, "", routeCapabilityHealthPolicy(), time.Now(), 0)
		return err
	}
	if !classification.MarkChannelModel {
		return nil
	}
	if classification.Class != RouteErrorTransient {
		retryAfter = 0
	}
	_, err := persistRouteHealthFailureWithRetryAfter(ctx, channelID, requestModel, "", DefaultRouteHealthPolicy(), time.Now(), retryAfter)
	return err
}

func RouteKeyScope(key string) string {
	sum := sha256.Sum256([]byte(key))
	return fmt.Sprintf("key:%x", sum[:])
}

func persistRouteHealthFailure(ctx context.Context, channelID int, requestModel, keyScope string, policy RouteHealthPolicy, now time.Time) (model.ChannelHealth, error) {
	return persistRouteHealthFailureWithRetryAfter(ctx, channelID, requestModel, keyScope, policy, now, 0)
}

func persistRouteHealthFailureWithRetryAfter(ctx context.Context, channelID int, requestModel, keyScope string, policy RouteHealthPolicy, now time.Time, retryAfter time.Duration) (model.ChannelHealth, error) {
	return persistRouteHealthObservation(ctx, channelID, requestModel, keyScope, now, func(health model.ChannelHealth) model.ChannelHealth {
		return ObserveRouteHealthFailureWithRetryAfter(health, policy, now, retryAfter)
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

func ObserveLiveRouteSuccessForKey(ctx context.Context, channelID int, requestModel, key string, latencyMS, ttftMS int64) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ObserveLiveRouteSuccessWithMetrics(ctx, channelID, requestModel, latencyMS, ttftMS); err != nil {
		return err
	}
	if strings.TrimSpace(key) == "" {
		return nil
	}
	now := time.Now()
	keyScope := RouteKeyScope(key)
	var existing model.ChannelHealth
	err := model.DB.WithContext(ctx).Where("channel_id = ? AND model = ? AND key_scope = ?", channelID, modellab.NormalizeModel(requestModel), keyScope).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = persistRouteHealthObservation(ctx, channelID, requestModel, keyScope, now, func(health model.ChannelHealth) model.ChannelHealth {
		return ObserveRouteHealthSuccess(health, now)
	})
	return err
}

func ObserveLiveRouteSuccessWithMetrics(ctx context.Context, channelID int, requestModel string, latencyMS, ttftMS int64) error {
	_, err := PersistRouteHealthSuccessWithMetrics(ctx, channelID, requestModel, time.Now(), latencyMS, ttftMS)
	return err
}

func UnavailableRouteKeyIndexes(ctx context.Context, channel *model.Channel, requestModel string, now time.Time) (map[int]struct{}, error) {
	excluded := make(map[int]struct{})
	if channel == nil {
		return excluded, nil
	}
	if model.DB == nil {
		return nil, errors.New("route health database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var healthRows []model.ChannelHealth
	if err := model.DB.WithContext(ctx).
		Where("channel_id = ? AND model = ? AND key_scope <> ?", channel.Id, modellab.NormalizeModel(requestModel), "").
		Find(&healthRows).Error; err != nil {
		return nil, err
	}
	byScope := make(map[string]model.ChannelHealth, len(healthRows))
	for _, health := range healthRows {
		byScope[health.KeyScope] = health
	}
	for index, key := range channel.GetKeys() {
		health, found := byScope[RouteKeyScope(key)]
		if found && !CanUseRouteHealth(health, now) {
			excluded[index] = struct{}{}
		}
	}
	return excluded, nil
}

func RouteChannelHasAvailableKey(ctx context.Context, channel *model.Channel, requestModel string, now time.Time) (bool, error) {
	if channel == nil {
		return false, nil
	}
	excluded, err := UnavailableRouteKeyIndexes(ctx, channel, requestModel, now)
	if err != nil {
		return false, err
	}
	for _, index := range channel.GetEnabledKeyIndexes() {
		if _, unavailable := excluded[index]; !unavailable {
			return true, nil
		}
	}
	return false, nil
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
		maxDurationSeconds := int64((time.Duration(1<<63 - 1)) / time.Second)
		if seconds > maxDurationSeconds {
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
	return ObserveRouteHealthFailureWithRetryAfter(health, policy, now, 0)
}

func ObserveRouteHealthFailureWithRetryAfter(health model.ChannelHealth, policy RouteHealthPolicy, now time.Time, retryAfter time.Duration) model.ChannelHealth {
	policy = policy.normalized()
	health.Normalize(now)
	health.FailureCount++
	// A provider Retry-After is an explicit shared cooldown signal. It must
	// fence concurrent requests even before the local consecutive-failure
	// threshold is reached.
	if health.State == model.RouteHealthStateHalfOpen || health.FailureCount >= policy.FailureThreshold || retryAfter > 0 {
		health.State = model.RouteHealthStateOpen
		health.CooldownUntil = now.Add(policy.Cooldown).Unix()
		if retryAfter > policy.Cooldown {
			health.CooldownUntil = now.Add(retryAfter).Unix()
		}
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
