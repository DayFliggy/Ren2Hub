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
	"gorm.io/gorm"
)

var (
	ErrLiveRouteProfileUnavailable = errors.New("live route profile is unavailable")
	ErrRoutePriceRatioExceeded     = errors.New("route policy maximum price ratio exceeded")
	ErrLiveRouteCandidateInvalid   = errors.New("live route candidate failed final qualification")
)

const RouteFilterKeyUnavailable = "key_unavailable"

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
	switch LiveRouteQualificationReason(err) {
	case ShadowFilterSnapshotUnavailable,
		ShadowFilterSnapshotStale,
		ShadowFilterUnknownCapability,
		ShadowFilterUnsupported,
		ShadowFilterChannelDisabled,
		ShadowFilterAbilityDisabled,
		ShadowFilterGroupForbidden,
		ShadowFilterPathUnsupported,
		ShadowFilterEntitlementRevoked,
		ShadowFilterMappingConflict,
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
	Context                  context.Context
	RouteSource              RouteSource
	UserID                   int
	TokenID                  int
	ChannelID                int
	RequestModel             string
	RequestPath              string
	UserGroup                string
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
	if model.DB == nil || input.UserID <= 0 || input.TokenID <= 0 || input.ChannelID <= 0 {
		return ErrLiveRouteProfileUnavailable
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	requestModel := strings.TrimSpace(input.RequestModel)
	normalizedModel := modellab.NormalizeModel(requestModel)
	if normalizedModel == "" {
		return &LiveRouteQualificationError{Reason: ShadowFilterUnknownCapability}
	}

	var channel model.Channel
	if err := model.DB.WithContext(ctx).Where("id = ?", input.ChannelID).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &LiveRouteQualificationError{Reason: ShadowFilterChannelDisabled}
		}
		return err
	}

	activeSnapshots, err := model.FindActiveChannelCapabilitySnapshots(ctx, []int{input.ChannelID})
	if err != nil {
		return err
	}
	if len(activeSnapshots) == 0 || activeSnapshots[0].ActiveVersion <= 0 {
		return &LiveRouteQualificationError{Reason: ShadowFilterSnapshotUnavailable}
	}
	activeSnapshot := activeSnapshots[0]
	if input.ExpectedSnapshotVersion <= 0 || activeSnapshot.ActiveVersion != input.ExpectedSnapshotVersion {
		return &LiveRouteQualificationError{Reason: ShadowFilterSnapshotStale}
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
		return &LiveRouteQualificationError{Reason: ShadowFilterUnknownCapability}
	}
	if input.ExpectedCatalogVersion != "" && capability.CatalogVersion != input.ExpectedCatalogVersion {
		return &LiveRouteQualificationError{Reason: ShadowFilterSnapshotStale}
	}

	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "group").Where("id = ?", input.UserID).First(&user).Error; err != nil {
		return err
	}
	var token model.Token
	if err := model.DB.WithContext(ctx).Where("id = ? AND user_id = ?", input.TokenID, input.UserID).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &LiveRouteQualificationError{Reason: ShadowFilterTokenForbidden}
		}
		return err
	}
	if token.Status != common.TokenStatusEnabled || (token.ExpiredTime != -1 && token.ExpiredTime <= common.GetTimestamp()) {
		return &LiveRouteQualificationError{Reason: ShadowFilterTokenForbidden}
	}
	// The authenticated context is an input to selection, but the current user
	// record is authoritative at the final execution boundary.
	input.UserGroup = user.Group

	var profile model.UserRouteProfile
	if err := model.DB.WithContext(ctx).Where("user_id = ? AND token_id = ?", input.UserID, input.TokenID).First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return &LiveRouteQualificationError{Reason: "profile_missing"}
		}
		return err
	}
	if profile.Status != model.RouteProfileStatusEnabled {
		return &LiveRouteQualificationError{Reason: "profile_disabled"}
	}
	if input.ExpectedProfileVersion > 0 && profile.Version != input.ExpectedProfileVersion {
		return &LiveRouteQualificationError{Reason: "configuration_stale"}
	}
	if input.RouteSource == RouteSourceManual {
		if profile.Mode != model.RouteModeManual || profile.ActiveGroupID == nil {
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
		if entry.Source != model.RouteSourcePlatform {
			return &LiveRouteQualificationError{Reason: "source_unsupported"}
		}
		if !entry.Enabled {
			return &LiveRouteQualificationError{Reason: "entry_disabled"}
		}
	} else if input.RouteSource == RouteSourceAutoLab && profile.Mode != model.RouteModeAutoLab {
		return &LiveRouteQualificationError{Reason: "profile_mode_mismatch"}
	}

	var abilities []model.Ability
	if err := model.DB.WithContext(ctx).Where("channel_id = ?", input.ChannelID).Find(&abilities).Error; err != nil {
		return err
	}
	abilityEnabled := false
	abilityAllowed := false
	abilityGroups := make([]string, 0, len(abilities))
	seenGroups := make(map[string]struct{}, len(abilities))
	for _, ability := range abilities {
		if modellab.NormalizeModel(ability.Model) != normalizedModel || !ability.Enabled {
			continue
		}
		abilityEnabled = true
		if _, exists := seenGroups[ability.Group]; !exists {
			abilityGroups = append(abilityGroups, ability.Group)
			seenGroups[ability.Group] = struct{}{}
		}
		if ability.Group == input.UserGroup || IsUserSelectableGroup(input.UserGroup, ability.Group) {
			abilityAllowed = true
		}
	}

	var entitlement model.UserChannelEntitlement
	entitlementErr := model.DB.WithContext(ctx).Where("user_id = ? AND channel_id = ? AND source = ?", input.UserID, input.ChannelID, model.RouteSourcePlatform).First(&entitlement).Error
	if entitlementErr != nil && !errors.Is(entitlementErr, gorm.ErrRecordNotFound) {
		return entitlementErr
	}
	entitled := errors.Is(entitlementErr, gorm.ErrRecordNotFound) || entitlementIsActive(entitlement)
	filterResult := filterRouteCapability(routeCapabilityFilterInput{
		Capability:               capability,
		SnapshotVersion:          activeSnapshot.ActiveVersion,
		ChannelStatus:            channel.Status,
		ChannelType:              channel.Type,
		AbilityEnabled:           abilityEnabled,
		AbilityAllowed:           abilityAllowed,
		AbilityGroups:            abilityGroups,
		UserGroup:                input.UserGroup,
		Token:                    token,
		TokenLimitEnabled:        token.ModelLimitsEnabled,
		TokenLimit:               token.GetModelLimitsMap(),
		RequestModel:             requestModel,
		NormalizedModel:          normalizedModel,
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

// LiveRouteRequest contains request facts already established by legacy auth
// and distribution middleware. It intentionally does not carry credentials,
// body content, or channel configuration.
type LiveRouteRequest struct {
	Context                  context.Context
	CapabilityEnabled        bool
	RequestID                string
	UserID                   int
	TokenID                  int
	RequestModel             string
	RequestPath              string
	UserGroup                string
	TokenModelLimitEnabled   bool
	TokenModelLimit          map[string]bool
	PriceEligibilityKnown    bool
	PriceEligible            bool
	SecurityEligibilityKnown bool
	SecurityAllowed          bool
}

type LiveRouteSelection struct {
	Source   RouteSource
	Decision RouteDecision
	Attempts []RouteDecisionCandidate
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
	budget := DefaultRouteRetryBudget()
	for index := currentAttempt + 1; index < len(selection.Attempts); index++ {
		candidate := selection.Attempts[index]
		if candidate.FilterReason != "" {
			continue
		}
		relation := RouteRetryFailover
		if candidate.ChannelID == currentChannelID {
			relation = RouteRetrySameChannel
		}
		if budget.Allows(class, relation, counters) {
			return candidate, index, true
		}
	}
	return RouteDecisionCandidate{}, 0, false
}

// SelectLiveTokenRoute is the side-effect-free route source bridge used by a
// future middleware integration. A missing or disabled profile preserves the
// legacy route; an enabled profile with no safe candidate fails closed.
func SelectLiveTokenRoute(input LiveRouteRequest) (LiveRouteSelection, error) {
	selection := LiveRouteSelection{Source: RouteSourceLegacy}
	if !input.CapabilityEnabled || input.UserID <= 0 || input.TokenID <= 0 {
		selection.Decision = NewRouteDecision("", RouteSourceLegacy, input.RequestModel, 0)
		return selection, nil
	}
	if input.Context == nil {
		input.Context = context.Background()
	}
	if model.DB == nil {
		return selection, ErrLiveRouteProfileUnavailable
	}
	var profile model.UserRouteProfile
	err := model.DB.WithContext(input.Context).
		Where("user_id = ? AND token_id = ?", input.UserID, input.TokenID).
		First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		selection.Decision = NewRouteDecision("", RouteSourceLegacy, input.RequestModel, 0)
		return selection, nil
	}
	if err != nil {
		return selection, err
	}
	if profile.Status != model.RouteProfileStatusEnabled {
		selection.Decision = NewRouteDecision("", RouteSourceLegacy, input.RequestModel, profile.Version)
		return selection, nil
	}
	source := ResolveRouteSource(RouteSourceInput{
		CapabilityEnabled: true,
		HasProfile:        true,
		ProfileMode:       profile.Mode,
	})
	selection.Source = source
	if source == RouteSourceLegacy {
		selection.Decision = NewRouteDecision("", source, input.RequestModel, profile.Version)
		return selection, nil
	}

	var candidates []RouteSelectionCandidate
	if source == RouteSourceManual {
		preview, previewErr := PreviewUserRouteProfile(input.Context, input.UserID, profile.ID, RouteProfilePreviewInput{
			Model: input.RequestModel,
			Path:  input.RequestPath,
		})
		if previewErr != nil {
			return selection, previewErr
		}
		for _, entry := range preview.Entries {
			candidates = append(candidates, RouteSelectionCandidate{
				ChannelID: entry.ChannelID, RequestModel: entry.RequestModel,
				ActualModel: entry.ActualModel, LabSlug: entry.LabSlug,
				Position: entry.Position, Weight: entry.Weight,
				FilterReason: entry.FilterReason, HealthUsable: true,
				SnapshotVersion: entry.SnapshotVersion, CatalogVersion: entry.CatalogVersion,
			})
		}
		if err := applyLiveHealth(input.Context, input.RequestModel, candidates); err != nil {
			return selection, err
		}
		result, selectErr := SelectTokenRoute(RouteSelectionInput{
			SourceInput:          RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: profile.Mode},
			ManualGroupEnabled:   preview.ActiveGroup != nil && preview.ActiveGroup.Enabled,
			ManualLoadBalance:    preview.Policy != nil && preview.Policy.LoadBalance,
			ManualCandidates:     candidates,
			ConfigurationVersion: profile.Version,
			RequestID:            input.RequestID,
			RequestModel:         input.RequestModel,
		})
		selection.Decision = result.Decision
		if preview.Policy != nil {
			selection.MaxRatio = preview.Policy.MaxRatio
			selection.Retry = RouteLiveRetryPolicy{
				Mode:                    preview.Policy.RetryMode,
				MaxSameResourceAttempts: preview.Policy.MaxSameResourceAttempts,
				MaxFailoverAttempts:     preview.Policy.MaxFailoverAttempts,
			}
		}
		selection.Attempts = manualRouteAttemptCandidates(result, selection.Retry)
		return selection, selectErr
	}

	entitlements, err := liveRouteEntitlements(input.Context, input.UserID)
	if err != nil {
		return selection, err
	}
	shadow := SelectRouteShadow(RouteShadowRequest{
		UserID:                   input.UserID,
		TokenID:                  input.TokenID,
		RequestModel:             input.RequestModel,
		NormalizedRequestModel:   modellab.NormalizeModel(input.RequestModel),
		RequestPath:              input.RequestPath,
		EndpointType:             endpointTypeForRequestPath(input.RequestPath),
		UserGroup:                input.UserGroup,
		TokenModelLimitEnabled:   input.TokenModelLimitEnabled,
		TokenModelLimit:          input.TokenModelLimit,
		EntitledChannels:         entitlements,
		PriceEligible:            input.PriceEligible,
		PriceEligibilityKnown:    input.PriceEligibilityKnown,
		SecurityAllowed:          input.SecurityAllowed,
		SecurityEligibilityKnown: input.SecurityEligibilityKnown,
	})
	statuses, err := liveRouteChannelStatuses(input.Context, shadow.ShadowCandidates)
	if err != nil {
		return selection, err
	}
	for _, candidate := range shadow.ShadowCandidates {
		filterReason := candidate.FilterReason
		if filterReason == "" && statuses[candidate.ChannelID] != common.ChannelStatusEnabled {
			filterReason = ShadowFilterChannelDisabled
		}
		candidates = append(candidates, RouteSelectionCandidate{
			ChannelID: candidate.ChannelID, RequestModel: candidate.RequestModel,
			ActualModel: candidate.ActualModel, LabSlug: candidate.LabSlug,
			Priority: candidate.Priority, Weight: candidate.Weight,
			FilterReason: filterReason, HealthUsable: true,
			SnapshotVersion: candidate.SnapshotVersion, CatalogVersion: candidate.CatalogVersion,
		})
	}
	if err := applyLiveHealth(input.Context, input.RequestModel, candidates); err != nil {
		return selection, err
	}
	result, selectErr := SelectTokenRoute(RouteSelectionInput{
		SourceInput:          RouteSourceInput{CapabilityEnabled: true, HasProfile: true, ProfileMode: profile.Mode},
		AutoCandidates:       candidates,
		TopK:                 3,
		ConfigurationVersion: profile.Version,
		RequestID:            shadow.RequestID,
		RequestModel:         input.RequestModel,
	})
	selection.Decision = result.Decision
	selection.Attempts = selectedRouteAttemptCandidates(result)
	if selectErr != nil {
		return selection, selectErr
	}
	return selection, nil
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

func applyLiveHealth(ctx context.Context, requestModel string, candidates []RouteSelectionCandidate) error {
	now := time.Now()
	for index := range candidates {
		if candidates[index].FilterReason != "" {
			continue
		}
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
		candidates[index].ErrorRate, candidates[index].LatencyMS, candidates[index].TTFTMS, err = RouteHealthMetricsWithTTFT(ctx, candidates[index].ChannelID, requestModel)
		if err != nil {
			return err
		}
	}
	return nil
}

func liveRouteChannelStatuses(ctx context.Context, candidates []RouteShadowCandidate) (map[int]int, error) {
	ids := make([]int, 0, len(candidates))
	seen := make(map[int]struct{}, len(candidates))
	for _, candidate := range candidates {
		if candidate.ChannelID <= 0 {
			continue
		}
		if _, exists := seen[candidate.ChannelID]; exists {
			continue
		}
		seen[candidate.ChannelID] = struct{}{}
		ids = append(ids, candidate.ChannelID)
	}
	statuses := make(map[int]int, len(ids))
	if len(ids) == 0 {
		return statuses, nil
	}
	var channels []model.Channel
	if err := model.DB.WithContext(ctx).Select("id", "status").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return nil, err
	}
	for _, channel := range channels {
		statuses[channel.Id] = channel.Status
	}
	return statuses, nil
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
