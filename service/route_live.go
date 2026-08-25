package service

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"gorm.io/gorm"
)

var (
	ErrLiveRouteProfileUnavailable = errors.New("live route profile is unavailable")
	ErrRoutePriceRatioExceeded     = errors.New("route policy maximum price ratio exceeded")
)

// LiveRouteRequest contains request facts already established by legacy auth
// and distribution middleware. It intentionally does not carry credentials,
// body content, or channel configuration.
type LiveRouteRequest struct {
	Context                context.Context
	CapabilityEnabled      bool
	RequestID              string
	UserID                 int
	TokenID                int
	RequestModel           string
	RequestPath            string
	UserGroup              string
	TokenModelLimitEnabled bool
	TokenModelLimit        map[string]bool
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
	if attempt < 0 || attempt >= len(selection.Attempts) {
		return RouteDecisionCandidate{}, false
	}
	return selection.Attempts[attempt], true
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
		UserID:                 input.UserID,
		TokenID:                input.TokenID,
		RequestModel:           input.RequestModel,
		NormalizedRequestModel: modellab.NormalizeModel(input.RequestModel),
		RequestPath:            input.RequestPath,
		EndpointType:           endpointTypeForRequestPath(input.RequestPath),
		UserGroup:              input.UserGroup,
		TokenModelLimitEnabled: input.TokenModelLimitEnabled,
		TokenModelLimit:        input.TokenModelLimit,
		EntitledChannels:       entitlements,
		PriceEligible:          true,
		SecurityAllowed:        true,
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
	available := selectedRouteAttemptCandidates(result)
	if len(available) == 0 {
		return available
	}
	if policy.Mode == "" {
		policy.Mode = model.RoutePolicyRetryNextChannel
	}
	maxAttempts := DefaultTotalAttempts
	appendCandidate := func(attempts []RouteDecisionCandidate, candidate RouteDecisionCandidate) []RouteDecisionCandidate {
		if len(attempts) >= maxAttempts {
			return attempts
		}
		return append(attempts, candidate)
	}
	appendSameResource := func(attempts []RouteDecisionCandidate, candidate RouteDecisionCandidate) []RouteDecisionCandidate {
		repetitions := policy.MaxSameResourceAttempts
		if repetitions < 0 {
			repetitions = 0
		}
		for index := 0; index <= repetitions && len(attempts) < maxAttempts; index++ {
			attempts = appendCandidate(attempts, candidate)
		}
		return attempts
	}

	attempts := make([]RouteDecisionCandidate, 0, maxAttempts)
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
		for index, candidate := range available {
			if index > failovers || len(attempts) >= maxAttempts {
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
		for index, candidate := range available {
			if index > failovers || len(attempts) >= maxAttempts {
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
