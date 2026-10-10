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
	"github.com/QuantumNous/new-api/relaykit/types"
	"gorm.io/gorm"
)

// RouteErrorClass is deliberately independent from provider-specific error
// types. The relay adapter can map its error into this small vocabulary before
// health and retry policy are consulted.
type RouteErrorClass string

const (
	RouteErrorInput      RouteErrorClass = "input"
	RouteErrorPermission RouteErrorClass = "permission"
	RouteErrorKey        RouteErrorClass = "key"
	RouteErrorTransient  RouteErrorClass = "provider_transient"
	RouteErrorModel      RouteErrorClass = "model_capability"
	RouteErrorAdmission  RouteErrorClass = "route_admission"
	RouteErrorUnknown    RouteErrorClass = "unknown"
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
// parsing. Status, the provider's error code and type, and a short message are
// the only inputs; no request body or credential is ever retained.
func ClassifyRouteError(status int, providerCode, providerType, message string, streamStarted bool) RouteErrorClassification {
	if strings.HasPrefix(providerCode, "route_lease") || strings.HasPrefix(providerCode, "route_admission") {
		return RouteErrorClassification{Class: RouteErrorAdmission}
	}
	classification := classifyProviderRouteError(status, providerCode, providerType, message)
	if streamStarted {
		classification.Retryable, classification.Failoverable = false, false
	}
	return classification
}

// RouteErrorType returns the error type the provider reported inside its error
// object (OpenAI and Anthropic "type"), which the error code does not carry.
// Errors without a provider error object report their local code here.
func RouteErrorType(apiErr *types.NewAPIError) string {
	if apiErr == nil {
		return ""
	}
	switch relayError := apiErr.RelayError.(type) {
	case types.OpenAIError:
		return relayError.Type
	case types.ClaudeError:
		return relayError.Type
	}
	return ""
}

var (
	routeKeyFailureText       = []string{"invalid api key", "incorrect api key"}
	routeKeyFailurePrefixes   = []string{"incorrect api key provided", "invalid api key"}
	routeModelFailureText     = []string{"model not found", "unsupported model", "invalid model"}
	routeModelFailurePrefixes = []string{"invalid model:", "model not found"}
)

func classifyProviderRouteError(status int, providerCode, providerType, message string) RouteErrorClassification {
	code := strings.ToLower(strings.TrimSpace(providerCode))
	kind := strings.ToLower(strings.TrimSpace(providerType))
	text := strings.ToLower(strings.TrimSpace(message))
	// Providers echo request values inside error messages ("Invalid value:
	// '...'"), so text found anywhere only confirms a class the status already
	// names. A message that starts with a failure phrase is the provider's own
	// wording (xAI answers a bad key with 400, Mistral a bad model with 400) and
	// counts at any status, unless the provider typed the error as a request
	// error: request validators start messages with the client's field name.
	providerWording := !strings.Contains(code, "invalid_request") && !strings.Contains(kind, "invalid_request")
	keyText := (status == 401 || status == 403) && containsAnyText(text, routeKeyFailureText) ||
		providerWording && hasAnyTextPrefix(text, routeKeyFailurePrefixes)
	modelText := status == 404 && containsAnyText(text, routeModelFailureText) ||
		providerWording && hasAnyTextPrefix(text, routeModelFailurePrefixes) ||
		// Anthropic: 404 not_found_error "model: <name>".
		status == 404 && strings.Contains(kind, "not_found_error") && strings.HasPrefix(text, "model:")
	switch {
	case strings.Contains(code, "invalid_api_key") || strings.Contains(code, "invalid_credential") ||
		strings.Contains(code, "authentication_error") || strings.Contains(kind, "authentication_error") || keyText:
		return RouteErrorClassification{Class: RouteErrorKey, Retryable: true, Failoverable: true, MarkKey: true}
	case strings.Contains(code, "model_not_found") || strings.Contains(code, "unsupported_model") || strings.Contains(code, "model_not_supported") || strings.Contains(code, "invalid_model") || modelText:
		return RouteErrorClassification{Class: RouteErrorModel, Failoverable: true, MarkCapability: true}
	// 401 is authentication whatever the code says; DeepSeek sends a bad key
	// as 401 with code invalid_request_error.
	case status == 401:
		return RouteErrorClassification{Class: RouteErrorKey, Retryable: true, Failoverable: true, MarkKey: true}
	case status == 400 || status == 422 || strings.Contains(code, "invalid_request"):
		return RouteErrorClassification{Class: RouteErrorInput}
	case status == 403 || strings.Contains(code, "permission"):
		return RouteErrorClassification{Class: RouteErrorPermission}
	case status == 408 || status == 409 || status == 425 || status == 429 || status >= 500 || strings.Contains(code, "rate_limit") || strings.Contains(code, "timeout"):
		return RouteErrorClassification{Class: RouteErrorTransient, Retryable: true, Failoverable: true, MarkChannelModel: true}
	default:
		return RouteErrorClassification{Class: RouteErrorUnknown, Retryable: status >= 500, Failoverable: status >= 500, MarkChannelModel: status >= 500}
	}
}

func containsAnyText(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func hasAnyTextPrefix(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.HasPrefix(text, phrase) {
			return true
		}
	}
	return false
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
		if health.ProbeExpiresAt > 0 {
			return health.ProbeExpiresAt <= now.UnixMilli()
		}
		return health.CooldownUntil <= now.Unix()
	}
	return health.State == model.RouteHealthStateOpen && health.CooldownUntil <= now.Unix()
}

// RouteHealthReadOnlyUsable never claims a probe. Actual admitted attempts use
// AcquireRouteAttemptHealth to fence recovery with an owner and expiry.
func RouteHealthReadOnlyUsable(health model.ChannelHealth, now time.Time) bool {
	return CanUseRouteHealth(health, now)
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
	health, err := LoadRouteHealth(ctx, channelID, requestModel)
	return RouteHealthReadOnlyUsable(health, now), health.HealthEpoch, err
}

// RouteKeyHealthUsable checks availability without claiming a recovery probe.
func RouteKeyHealthUsable(ctx context.Context, channelID int, requestModel, key string, now time.Time) (bool, int64, error) {
	if strings.TrimSpace(key) == "" {
		return false, 0, nil
	}
	health, err := loadRouteHealthScope(ctx, channelID, requestModel, RouteKeyScope(key))
	return RouteHealthReadOnlyUsable(health, now), health.HealthEpoch, err
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

// Ranking band thresholds. A single source keeps the observed band, the
// filter floor, and the weight factor from drifting apart, which is a failure
// mode the reference implementation exhibits with duplicated defaults.
const (
	// RouteRankMinimumSamples gates both bands. Below it a channel is unknown
	// rather than assumed perfect, so a cold or newly added channel is neither
	// rewarded nor punished by an unrepresentative sample.
	RouteRankMinimumSamples = 50
	RouteRankErrorWarning   = 0.05
	RouteRankErrorCritical  = 0.20
	RouteRankTTFTTargetMS   = 3000
	RouteRankTTFTCriticalMS = 10000
)

// RouteRankBand is an ordinal health band. Bands keep priority authoritative:
// they are compared only inside one priority layer, so a degraded high
// priority channel is still preferred over a healthy lower priority one.
type RouteRankBand int

const (
	RouteRankBandGood RouteRankBand = iota
	RouteRankBandWarning
	RouteRankBandCritical
)

// RouteHealthRankMetrics carries the ordinal bands used for ordering. Unknown
// bands mean "insufficient observation", not "healthy".
type RouteHealthRankMetrics struct {
	ErrorBand      RouteRankBand
	ErrorBandKnown bool
	TTFTBand       RouteRankBand
	TTFTBandKnown  bool
}

// RouteHealthRankingMetrics derives ranking bands from cumulative counters.
// Callers must pass a channel-scoped row (key_scope ""): key-scoped rows are
// written by failure-only paths and would report an inflated error rate.
func RouteHealthRankingMetrics(health model.ChannelHealth) RouteHealthRankMetrics {
	metrics := RouteHealthRankMetrics{}
	if health.ID == 0 || health.KeyScope != "" {
		return metrics
	}
	if health.ObservedRequests >= RouteRankMinimumSamples {
		rate := float64(health.ObservedFailures) / float64(health.ObservedRequests)
		metrics.ErrorBand, metrics.ErrorBandKnown = rankBandForValue(rate, RouteRankErrorWarning, RouteRankErrorCritical), true
	}
	if health.TTFTObservationCnt >= RouteRankMinimumSamples {
		average := float64(health.TTFTSumMS) / float64(health.TTFTObservationCnt)
		metrics.TTFTBand, metrics.TTFTBandKnown = rankBandForValue(average, RouteRankTTFTTargetMS, RouteRankTTFTCriticalMS), true
	}
	return metrics
}

// rankBandForValue maps a measured value onto a band. Lower is better, so the
// warning floor is the first boundary and the critical floor the second.
func rankBandForValue(value, warning, critical float64) RouteRankBand {
	if value >= critical {
		return RouteRankBandCritical
	}
	if value >= warning {
		return RouteRankBandWarning
	}
	return RouteRankBandGood
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
			health.TTFTSumMS += ttftMS
			health.TTFTObservationCnt++
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
	classification := ClassifyRouteError(status, providerCode, "", message, streamStarted)
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
		err := model.LockForUpdate(tx).
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

// RouteChannelHasReadOnlyAvailableKey is the non-mutating counterpart used by
// preview surfaces. An expired open cooldown may be displayed as available;
// the actual relay still atomically claims its half-open probe.
func RouteChannelHasReadOnlyAvailableKey(ctx context.Context, channel *model.Channel, requestModel string, now time.Time) (bool, error) {
	return RouteChannelHasAvailableKey(ctx, channel, requestModel, now)
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

// RouteRetryAfterCap bounds every Retry-After this gateway accepts. The value
// becomes an explicit cooldown on the channel x model health row and on the
// provider rate-limit scope, and neither has a manual clear entry point, so an
// absurd upstream or CDN value must not park a route for days.
//
// Five minutes matches the longest cooldown the local health policies apply
// (key and capability failures; the default policy uses thirty seconds). When a
// capped cooldown ends, one half-open probe is admitted; if the provider is
// still limiting, its next Retry-After re-arms the cooldown, so a longer real
// wait costs at most one probe per window. Within the cap, Retry-After remains
// a lower bound that callers never shorten.
const RouteRetryAfterCap = 5 * time.Minute

// ParseRetryAfter accepts the RFC 9110 delta-seconds and HTTP-date forms.
// Invalid or negative values are ignored; every usable value is capped by
// RouteRetryAfterCap, and the caller applies the remaining request deadline as
// the final upper bound.
func ParseRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err == nil || errors.Is(err, strconv.ErrRange) {
		// strconv saturates on range overflow: a too-large positive digit string
		// reports MaxInt64, a too-large negative reports MinInt64. Both keep the
		// sign of the original value, which is all this branch needs.
		if seconds < 0 {
			return 0, false
		}
		if seconds >= int64(RouteRetryAfterCap/time.Second) {
			return RouteRetryAfterCap, true
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
	until := when.Sub(now)
	if until > RouteRetryAfterCap {
		return RouteRetryAfterCap, true
	}
	return until, true
}

func ObserveRouteHealthFailure(health model.ChannelHealth, policy RouteHealthPolicy, now time.Time) model.ChannelHealth {
	return ObserveRouteHealthFailureWithRetryAfter(health, policy, now, 0)
}

func ObserveRouteHealthFailureWithRetryAfter(health model.ChannelHealth, policy RouteHealthPolicy, now time.Time, retryAfter time.Duration) model.ChannelHealth {
	policy = policy.normalized()
	health.Normalize(now)
	health.FailureCount++
	// Ranking counters are cumulative and keyed to the same row as the gate.
	// Recording them here keeps one write path for both concerns.
	health.ObservedRequests++
	health.ObservedFailures++
	// A provider Retry-After is an explicit shared cooldown signal. It must
	// fence concurrent requests even before the local consecutive-failure
	// threshold is reached.
	if health.State == model.RouteHealthStateHalfOpen || health.FailureCount >= policy.FailureThreshold || retryAfter > 0 {
		health.State = model.RouteHealthStateOpen
		cooldownUntil := now.Add(policy.Cooldown).Unix()
		if retryAfter > policy.Cooldown {
			cooldownUntil = now.Add(retryAfter).Unix()
		}
		health.CooldownUntil = max(health.CooldownUntil, cooldownUntil)
	}
	health.ProbeOwner, health.ProbeExpiresAt = "", 0
	health.HealthEpoch++
	health.UpdatedAt = now.Unix()
	return health
}

func ObserveRouteHealthSuccess(health model.ChannelHealth, now time.Time) model.ChannelHealth {
	health.Normalize(now)
	health.ObservedRequests++
	stateChanged := health.State != model.RouteHealthStateClosed || health.FailureCount != 0 || health.CooldownUntil != 0
	health.State = model.RouteHealthStateClosed
	health.FailureCount = 0
	health.CooldownUntil = 0
	health.ProbeOwner, health.ProbeExpiresAt = "", 0
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
