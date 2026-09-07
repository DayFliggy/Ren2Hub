package service

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"gorm.io/gorm"
)

var (
	ErrLiveRouteProfileUnavailable = errors.New("live route profile is unavailable")
	ErrRoutePriceRatioExceeded     = errors.New("route policy maximum price ratio exceeded")
	ErrLiveRouteCandidateInvalid   = errors.New("live route candidate failed final qualification")
)

const RouteFilterKeyUnavailable = "key_unavailable"

// RouteLiveSelectionRequiredContextKey marks requests that require a route
// decision and distributed admission before upstream execution.
const RouteLiveSelectionRequiredContextKey = "route_live_selection_required"

// LiveRouteQualificationError identifies a mutable authorization or
// capability fact that changed after selection. The reason is a stable
// internal enum and never contains provider data or credentials.
type LiveRouteQualificationError struct {
	Reason string
}

func (err *LiveRouteQualificationError) Error() string {
	if err == nil || err.Reason == "" {
		return ErrLiveRouteCandidateInvalid.Error()
	}
	return "live route candidate failed final qualification: " + err.Reason
}

func (err *LiveRouteQualificationError) Unwrap() error {
	return ErrLiveRouteCandidateInvalid
}

func LiveRouteQualificationReason(err error) string {
	var qualificationErr *LiveRouteQualificationError
	if errors.As(err, &qualificationErr) && qualificationErr != nil {
		return qualificationErr.Reason
	}
	return ""
}

// LiveRouteQualificationAllowsFailover distinguishes a stale or revoked
// candidate from a request-wide authorization or infrastructure failure.
// Only the former may consume the bounded next-candidate budget.
func LiveRouteQualificationAllowsFailover(err error) bool {
	if errors.Is(err, ErrRouteLeaseCapacity) {
		return true
	}
	switch LiveRouteQualificationReason(err) {
	case RouteFilterSnapshotUnavailable,
		RouteFilterSnapshotStale,
		RouteFilterUnknownCapability,
		RouteFilterUnsupported,
		RouteFilterChannelDisabled,
		RouteFilterAbilityDisabled,
		RouteFilterPathUnsupported,
		RouteFilterEntitlementRevoked,
		RouteFilterMappingConflict,
		"configuration_stale",
		"group_disabled",
		"entry_missing",
		"entry_disabled",
		"source_unsupported":
		return true
	default:
		return false
	}
}

type LiveRouteCandidateQualificationRequest struct {
	IsPlayground             bool
	SpecificChannelID        int
	PermissionModel          string
	Context                  context.Context
	RouteSource              RouteSource
	UserID                   int
	TokenID                  int
	ChannelID                int
	RequestModel             string
	RequestPath              string
	ExpectedSnapshotVersion  int64
	ExpectedCatalogVersion   string
	ExpectedProfileVersion   int64
	PriceEligibilityKnown    bool
	PriceEligible            bool
	SecurityEligibilityKnown bool
	SecurityAllowed          bool
}

// RecheckLiveRouteCandidate is the final qualification boundary after a live
// route lease has been acquired and immediately before billing/upstream
// execution. It re-reads mutable authorization and capability facts instead
// of trusting the selector snapshot or the request-time channel cache.
func RecheckLiveRouteCandidate(input LiveRouteCandidateQualificationRequest) error {
	if model.DB == nil || input.UserID <= 0 || input.ChannelID <= 0 {
		return ErrLiveRouteProfileUnavailable
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	requestModel := strings.TrimSpace(input.RequestModel)
	normalizedModel := modellab.NormalizeModel(requestModel)
	if normalizedModel == "" {
		return &LiveRouteQualificationError{Reason: RouteFilterUnknownCapability}
	}

	var channel model.Channel
	if err := model.DB.WithContext(ctx).Where("id = ?", input.ChannelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &LiveRouteQualificationError{Reason: RouteFilterChannelDisabled}
		}
		return err
	}

	activeSnapshots, err := model.FindActiveChannelCapabilitySnapshots(ctx, []int{input.ChannelID})
	if err != nil {
		return err
	}
	if len(activeSnapshots) == 0 || activeSnapshots[0].ActiveVersion <= 0 {
		return &LiveRouteQualificationError{Reason: RouteFilterSnapshotUnavailable}
	}
	activeSnapshot := activeSnapshots[0]
	if input.ExpectedSnapshotVersion <= 0 || activeSnapshot.ActiveVersion != input.ExpectedSnapshotVersion {
		return &LiveRouteQualificationError{Reason: RouteFilterSnapshotStale}
	}

	capabilities, err := model.FindActiveChannelCapabilities(ctx, []int{input.ChannelID}, normalizedModel, "")
	if err != nil {
		return err
	}
	var capability model.ChannelModelCapability
	for _, candidate := range capabilities {
		if candidate.ChannelID == input.ChannelID && candidate.RequestModel == normalizedModel {
			capability = candidate
			break
		}
	}
	if capability.ChannelID == 0 {
		return &LiveRouteQualificationError{Reason: RouteFilterUnknownCapability}
	}
	if input.ExpectedCatalogVersion != "" && capability.CatalogVersion != input.ExpectedCatalogVersion {
		return &LiveRouteQualificationError{Reason: RouteFilterSnapshotStale}
	}

	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "status").Where("id = ?", input.UserID).First(&user).Error; err != nil {
		return err
	}
	if user.Status != common.UserStatusEnabled {
		return &LiveRouteQualificationError{Reason: RouteFilterTokenForbidden}
	}
	var token model.Token
	if input.TokenID > 0 {
		if err := model.DB.WithContext(ctx).Where("id = ? AND user_id = ?", input.TokenID, input.UserID).First(&token).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return &LiveRouteQualificationError{Reason: RouteFilterTokenForbidden}
			}
			return err
		}
		if !routeTokenUsable(token) {
			return &LiveRouteQualificationError{Reason: RouteFilterTokenForbidden}
		}
	} else if !input.IsPlayground {
		return &LiveRouteQualificationError{Reason: RouteFilterTokenForbidden}
	}

	if input.SpecificChannelID > 0 {
		if input.ChannelID != input.SpecificChannelID {
			return &LiveRouteQualificationError{Reason: "origin_channel_mismatch"}
		}
	} else if input.TokenID > 0 {
		var profile model.UserRouteProfile
		err := model.DB.WithContext(ctx).Where("user_id = ? AND token_id = ?", input.UserID, input.TokenID).First(&profile).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.RouteSource != RouteSourceAutoLab || input.ExpectedProfileVersion != 0 {
				return &LiveRouteQualificationError{Reason: "profile_missing"}
			}
		} else if err != nil {
			return err
		} else {
			if profile.Status != model.RouteProfileStatusEnabled {
				return &LiveRouteQualificationError{Reason: "profile_disabled"}
			}
			if profile.Version != input.ExpectedProfileVersion {
				return &LiveRouteQualificationError{Reason: "configuration_stale"}
			}
			if string(input.RouteSource) != profile.Mode {
				return &LiveRouteQualificationError{Reason: "profile_mode_mismatch"}
			}
			if input.RouteSource == RouteSourceManual {
				if profile.ActiveGroupID == nil {
					return &LiveRouteQualificationError{Reason: "active_group_missing"}
				}
				var group model.UserRouteGroup
				if err := model.DB.WithContext(ctx).Where("id = ? AND profile_id = ?", *profile.ActiveGroupID, profile.ID).First(&group).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return &LiveRouteQualificationError{Reason: "active_group_missing"}
					}
					return err
				}
				if !group.Enabled {
					return &LiveRouteQualificationError{Reason: "group_disabled"}
				}
				var entry model.UserRouteEntry
				if err := model.DB.WithContext(ctx).Where("group_id = ? AND channel_id = ?", group.ID, input.ChannelID).First(&entry).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return &LiveRouteQualificationError{Reason: "entry_missing"}
					}
					return err
				}
				if entry.Source != model.RouteSourcePlatform || !entry.Enabled {
					return &LiveRouteQualificationError{Reason: "entry_disabled"}
				}
			}
		}
	}

	var abilities []model.Ability
	if err := model.DB.WithContext(ctx).Where("channel_id = ?", input.ChannelID).Find(&abilities).Error; err != nil {
		return err
	}
	abilityEnabled := false
	for _, ability := range abilities {
		if modellab.NormalizeModel(ability.Model) != normalizedModel || !ability.Enabled {
			continue
		}
		abilityEnabled = true
		break
	}

	var entitlement model.UserChannelEntitlement
	entitlementErr := model.DB.WithContext(ctx).Where("user_id = ? AND channel_id = ? AND source = ?", input.UserID, input.ChannelID, model.RouteSourcePlatform).First(&entitlement).Error
	if entitlementErr != nil && !errors.Is(entitlementErr, gorm.ErrRecordNotFound) {
		return entitlementErr
	}
	entitled := errors.Is(entitlementErr, gorm.ErrRecordNotFound) || entitlementIsActive(entitlement)
	permissionModel := normalizedModel
	if input.PermissionModel != "" {
		permissionModel = modellab.NormalizeModel(input.PermissionModel)
	}
	filterResult := filterRouteCapability(routeCapabilityFilterInput{
		Capability:               capability,
		SnapshotVersion:          activeSnapshot.ActiveVersion,
		ChannelStatus:            channel.Status,
		ChannelType:              channel.Type,
		AbilityEnabled:           abilityEnabled,
		Token:                    token,
		TokenLimitEnabled:        token.ModelLimitsEnabled,
		TokenLimit:               token.GetModelLimitsMap(),
		RequestModel:             requestModel,
		NormalizedModel:          permissionModel,
		RequestPath:              input.RequestPath,
		EndpointType:             endpointTypeForRequestPath(input.RequestPath),
		Entitled:                 entitled,
		PriceEligible:            input.PriceEligible,
		PriceEligibilityKnown:    input.PriceEligibilityKnown,
		SecurityAllowed:          input.SecurityAllowed,
		SecurityEligibilityKnown: input.SecurityEligibilityKnown,
		RequireSnapshot:          true,
		RequireEndpoint:          true,
	})
	if filterResult.Reason != "" {
		return &LiveRouteQualificationError{Reason: filterResult.Reason}
	}
	return nil
}

// LiveRouteRequest contains request facts already established by auth
// and distribution middleware. It intentionally does not carry credentials,
// body content, or channel configuration.
type LiveRouteRequest struct {
	ExcludedChannelIDs       map[int]string
	SpecificChannelID        int
	IsPlayground             bool
	NativeResponsesRequired  bool
	CompactStage             relaycommon.CompactAttemptStage
	ExcludedKeyIndexes       map[int]map[int]struct{}
	Context                  context.Context
	CapabilityEnabled        bool
	RequestID                string
	UserID                   int
	TokenID                  int
	RequestModel             string
	RequestPath              string
	TokenModelLimitEnabled   bool
	TokenModelLimit          map[string]bool
	PriceEligibilityKnown    bool
	PriceEligible            bool
	SecurityEligibilityKnown bool
	SecurityAllowed          bool
	PreferredChannelID       int
}

type LiveRouteSelection struct {
	CompactStage      relaycommon.CompactAttemptStage
	SpecificChannelID int
	Source            RouteSource
	Decision          RouteDecision
	Attempts          []RouteDecisionCandidate
	// MaxRatio is a manual-profile admission ceiling. It is evaluated against
	// the existing billing calculation at request time and never rewrites the
	// selected channel, billing model, or group ratio.
	MaxRatio float64
	Retry    RouteLiveRetryPolicy
}

// RouteLiveRetryPolicy is the validated manual-policy subset used by the
// relay attempt loop. It only narrows the system attempt budget; user-owned
// configuration can never expand the guarded live-route maximum.
type RouteLiveRetryPolicy struct {
	Mode                    string
	MaxSameResourceAttempts int
	MaxFailoverAttempts     int
}

func (policy RouteLiveRetryPolicy) Budget() RouteRetryBudget {
	if policy.Mode == "" {
		return DefaultRouteRetryBudget()
	}
	sameChannel := policy.MaxSameResourceAttempts
	if sameChannel < 0 {
		sameChannel = 0
	}
	if sameChannel > DefaultSameChannelAttempts {
		sameChannel = DefaultSameChannelAttempts
	}
	failover := policy.MaxFailoverAttempts
	if failover < 0 {
		failover = 0
	}
	if failover > DefaultFailoverAttempts {
		failover = DefaultFailoverAttempts
	}
	return RouteRetryBudget{
		SameKeyAttempts:     DefaultSameKeyAttempts,
		SameChannelAttempts: sameChannel,
		FailoverAttempts:    failover,
		TotalAttempts:       DefaultTotalAttempts,
	}
}

func (selection LiveRouteSelection) AllowsPriceRatio(actualRatio float64) bool {
	if selection.Source != RouteSourceManual || selection.MaxRatio <= 0 {
		return true
	}
	if math.IsNaN(actualRatio) || math.IsInf(actualRatio, 0) || actualRatio < 0 {
		return false
	}
	return actualRatio <= selection.MaxRatio
}

func (selection LiveRouteSelection) CandidateForAttempt(attempt int) (RouteDecisionCandidate, bool) {
	candidate, _, found := selection.CandidateAtOrAfter(attempt)
	return candidate, found
}

func (selection LiveRouteSelection) CandidateAtOrAfter(attempt int) (RouteDecisionCandidate, int, bool) {
	if attempt < 0 {
		attempt = 0
	}
	for index := attempt; index < len(selection.Attempts); index++ {
		if selection.Attempts[index].FilterReason == "" {
			return selection.Attempts[index], index, true
		}
	}
	return RouteDecisionCandidate{}, 0, false
}

func (selection LiveRouteSelection) NextCandidateForError(currentAttempt, currentChannelID int, class RouteErrorClassification, counters RouteRetryCounters) (RouteDecisionCandidate, int, bool) {
	if selection.Retry.Mode == model.RoutePolicyRetryNone {
		return RouteDecisionCandidate{}, 0, false
	}
	budget := selection.Retry.Budget()
	for index := currentAttempt + 1; index < len(selection.Attempts); index++ {
		candidate := selection.Attempts[index]
		if candidate.FilterReason != "" {
			continue
		}
		relation := RouteRetryFailover
		if candidate.ChannelID == currentChannelID {
			relation = RouteRetrySameChannel
		}
		if selection.Retry.Mode == model.RoutePolicyRetrySameChannel && relation != RouteRetrySameChannel {
			continue
		}
		if selection.Retry.Mode == model.RoutePolicyRetryNextChannel && relation != RouteRetryFailover {
			continue
		}
		if budget.Allows(class, relation, counters) {
			return candidate, index, true
		}
	}
	return RouteDecisionCandidate{}, 0, false
}

func selectedRouteAttemptCandidates(result RouteSelectionResult) []RouteDecisionCandidate {
	available := availableRouteAttemptCandidates(result)
	attempts := make([]RouteDecisionCandidate, 0, len(available)*2)
	for _, candidate := range available {
		attempts = append(attempts, candidate)
		if DefaultSameChannelAttempts > 0 {
			attempts = append(attempts, candidate)
		}
	}
	return attempts
}

func availableRouteAttemptCandidates(result RouteSelectionResult) []RouteDecisionCandidate {
	attempts := make([]RouteDecisionCandidate, 0, len(result.Candidates))
	for _, candidate := range result.Candidates {
		for _, decisionCandidate := range result.Decision.Candidates {
			if decisionCandidate.ChannelID == candidate.ChannelID && decisionCandidate.FilterReason == "" {
				attempts = append(attempts, decisionCandidate)
				break
			}
		}
	}
	return attempts
}

func manualRouteAttemptCandidates(result RouteSelectionResult, policy RouteLiveRetryPolicy) []RouteDecisionCandidate {
	available := availableRouteAttemptCandidates(result)
	if len(available) == 0 {
		return available
	}
	if policy.Mode == "" {
		policy.Mode = model.RoutePolicyRetryNextChannel
	}
	appendCandidate := func(attempts []RouteDecisionCandidate, candidate RouteDecisionCandidate) []RouteDecisionCandidate {
		return append(attempts, candidate)
	}
	appendSameResource := func(attempts []RouteDecisionCandidate, candidate RouteDecisionCandidate) []RouteDecisionCandidate {
		repetitions := policy.MaxSameResourceAttempts
		if repetitions < 0 {
			repetitions = 0
		}
		if repetitions > DefaultSameChannelAttempts {
			repetitions = DefaultSameChannelAttempts
		}
		for index := 0; index <= repetitions; index++ {
			attempts = appendCandidate(attempts, candidate)
		}
		return attempts
	}

	attempts := make([]RouteDecisionCandidate, 0, DefaultTotalAttempts)
	switch policy.Mode {
	case model.RoutePolicyRetryNone:
		return appendCandidate(attempts, available[0])
	case model.RoutePolicyRetrySameChannel:
		return appendSameResource(attempts, available[0])
	case model.RoutePolicyRetrySameThenNext:
		failovers := policy.MaxFailoverAttempts
		if failovers < 0 {
			failovers = 0
		}
		if failovers > DefaultFailoverAttempts {
			failovers = DefaultFailoverAttempts
		}
		for index, candidate := range available {
			if index > failovers {
				break
			}
			attempts = appendSameResource(attempts, candidate)
		}
		return attempts
	case model.RoutePolicyRetryNextChannel:
		fallthrough
	default:
		failovers := policy.MaxFailoverAttempts
		if failovers < 0 {
			failovers = 0
		}
		if failovers > DefaultFailoverAttempts {
			failovers = DefaultFailoverAttempts
		}
		for index, candidate := range available {
			if index > failovers {
				break
			}
			attempts = appendCandidate(attempts, candidate)
		}
		return attempts
	}
}

func applyLiveHealth(ctx context.Context, candidates []RouteSelectionCandidate) error {
	now := time.Now()
	for index := range candidates {
		if candidates[index].FilterReason != "" {
			continue
		}
		requestModel := candidates[index].RequestModel
		usable, epoch, err := RouteHealthUsable(ctx, candidates[index].ChannelID, requestModel, now)
		if err != nil {
			return err
		}
		if !usable {
			candidates[index].FilterReason = RouteCandidateFilterHealthUnavailable
			candidates[index].HealthUsable = false
			continue
		}
		channel, err := model.CacheGetChannel(candidates[index].ChannelID)
		if err != nil {
			return err
		}
		hasAvailableKey, err := RouteChannelHasAvailableKey(ctx, channel, requestModel, now)
		if err != nil {
			return err
		}
		if !hasAvailableKey {
			candidates[index].FilterReason = RouteFilterKeyUnavailable
			candidates[index].HealthUsable = false
			continue
		}
		candidates[index].HealthUsable = true
		candidates[index].HealthEpoch = epoch
		metrics, metricsErr := RouteHealthScoringMetrics(ctx, candidates[index].ChannelID, requestModel)
		if metricsErr != nil {
			return metricsErr
		}
		candidates[index].ErrorRate = metrics.ErrorRate
		candidates[index].ErrorRateKnown = metrics.ErrorRateKnown
		candidates[index].LatencyMS = metrics.LatencyMS
		candidates[index].LatencyKnown = metrics.LatencyKnown
		candidates[index].TTFTMS = metrics.TTFTMS
		candidates[index].TTFTKnown = metrics.TTFTKnown
		{
			runtimeMetrics, runtimeErr := LoadRouteScoreRuntimeMetrics(ctx, candidates[index].ChannelID, requestModel)
			if runtimeErr != nil {
				return runtimeErr
			}
			candidates[index].RateLimitHeadroom = runtimeMetrics.RateLimitHeadroom
			candidates[index].RateLimitKnown = runtimeMetrics.RateLimitKnown
			candidates[index].QuotaHeadroom = runtimeMetrics.QuotaHeadroom
			candidates[index].QuotaKnown = runtimeMetrics.QuotaKnown
		}
	}
	return nil
}

func liveRouteEntitlements(ctx context.Context, userID int) (map[int]bool, error) {
	if userID <= 0 || model.DB == nil {
		return nil, ErrLiveRouteProfileUnavailable
	}
	var rows []model.UserChannelEntitlement
	if err := model.DB.WithContext(ctx).Where("user_id = ? AND source = ?", userID, model.RouteSourcePlatform).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[int]bool, len(rows))
	for _, row := range rows {
		result[row.ChannelID] = row.Status == model.RouteEntitlementStatusEnabled && row.RevokedAt == 0 &&
			(row.ExpiresAt == 0 || row.ExpiresAt > common.GetTimestamp())
	}
	return result, nil
}
