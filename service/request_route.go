package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/reasoning"
	"gorm.io/gorm"
)

// SelectLiveTokenRoute is the single request planner for every relay protocol.
// A token without a private profile uses automatic routing; missing candidates
// or infrastructure never invoke another selector.
func SelectLiveTokenRoute(input LiveRouteRequest) (LiveRouteSelection, error) {
	selection := LiveRouteSelection{Source: RouteSourceUnavailable, SpecificChannelID: input.SpecificChannelID, ConversionPlans: make(map[int]relayconvert.ConversionPlan)}
	if !input.CapabilityEnabled || model.DB == nil || input.UserID <= 0 || (input.TokenID <= 0 && !input.IsPlayground) {
		return selection, ErrLiveRouteProfileUnavailable
	}
	if input.Context == nil {
		input.Context = context.Background()
	}
	profile := model.UserRouteProfile{Mode: model.RouteModeAutoLab}
	hasProfile := false
	if input.TokenID > 0 && input.SpecificChannelID == 0 {
		err := model.DB.WithContext(input.Context).Where("user_id = ? AND token_id = ?", input.UserID, input.TokenID).First(&profile).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return selection, err
		}
		hasProfile = err == nil
		if hasProfile && profile.Status != model.RouteProfileStatusEnabled {
			return selection, ErrRouteSelectionUnavailable
		}
	}
	sourceInput := RouteSourceInput{CapabilityEnabled: true, HasProfile: hasProfile, ProfileMode: profile.Mode}
	selection.Source = ResolveRouteSource(sourceInput)
	if selection.Source == RouteSourceUnavailable {
		return selection, ErrRouteProfileValidation
	}
	selection.Decision = NewRouteDecision(input.RequestID, selection.Source, input.RequestModel, profile.Version)

	entries := make(map[int]model.UserRouteEntry)
	manualEnabled, loadBalance := false, false
	if selection.Source == RouteSourceManual {
		view, err := GetUserRouteProfile(input.UserID, profile.ID)
		if err != nil {
			return selection, err
		}
		group := findActiveRouteGroup(view)
		if group == nil || !group.Group.Enabled {
			return selection, ErrRouteSelectionUnavailable
		}
		manualEnabled, loadBalance = true, group.Policy.LoadBalance
		selection.ManualLoadBalance = loadBalance
		selection.MaxRatio = group.Policy.MaxRatio
		selection.Retry = RouteLiveRetryPolicy{Mode: group.Policy.RetryMode, MaxSameResourceAttempts: group.Policy.MaxSameResourceAttempts, MaxFailoverAttempts: group.Policy.MaxFailoverAttempts}
		if !group.Policy.Sticky {
			input.PreferredChannelID = 0
		}
		for _, entry := range group.Entries {
			entries[entry.ChannelID] = entry
		}
	}

	index, _ := routeCapabilityIndex.Load().(*capabilityIndex)
	if index == nil {
		return selection, ErrRouteSelectionUnavailable
	}
	models := []string{modellab.NormalizeModel(input.RequestModel)}
	if base := modellab.NormalizeModel(reasoning.BaseModelName(input.RequestModel)); base != models[0] {
		models = append(models, base)
	}
	entitlements, err := liveRouteEntitlements(input.Context, input.UserID)
	if err != nil {
		return selection, err
	}
	channelIDs := make([]int, 0)
	seenChannels := make(map[int]bool)
	for _, lookupModel := range models {
		for _, indexed := range index.ByRequestModel[lookupModel] {
			channelID := indexed.Capability.ChannelID
			if input.SpecificChannelID > 0 && channelID != input.SpecificChannelID {
				continue
			}
			if _, manual := entries[channelID]; selection.Source == RouteSourceManual && !manual {
				continue
			}
			if !seenChannels[channelID] {
				seenChannels[channelID] = true
				channelIDs = append(channelIDs, channelID)
			}
		}
	}
	var channelRows []model.Channel
	if len(channelIDs) > 0 {
		if err := model.DB.WithContext(input.Context).Where("id IN ?", channelIDs).Find(&channelRows).Error; err != nil {
			return selection, err
		}
	}
	channels := make(map[int]*model.Channel, len(channelRows))
	for row := range channelRows {
		channels[channelRows[row].Id] = &channelRows[row]
	}
	// The index discovers channels; current rows decide qualification. A stale
	// index must not spend the candidate budget on revoked or obsolete entries.
	currentCapabilities, err := model.FindActiveChannelCapabilities(input.Context, channelIDs, "", "")
	if err != nil {
		return selection, err
	}
	type capabilityKey struct {
		channelID int
		model     string
	}
	currentByModel := make(map[capabilityKey]model.ChannelModelCapability, len(currentCapabilities))
	for _, capability := range currentCapabilities {
		currentByModel[capabilityKey{capability.ChannelID, capability.RequestModel}] = capability
	}
	abilityAccess, err := FindUserRouteCapabilityAccess(input.Context, input.UserID, currentCapabilities)
	if err != nil {
		return selection, err
	}
	candidates := make([]RouteSelectionCandidate, 0)
	byChannel := make(map[int]int)
	for _, lookupModel := range models {
		for _, indexed := range index.ByRequestModel[lookupModel] {
			capability := indexed.Capability
			if input.SpecificChannelID > 0 && capability.ChannelID != input.SpecificChannelID {
				continue
			}
			entry, inManual := entries[capability.ChannelID]
			if selection.Source == RouteSourceManual && !inManual {
				continue
			}
			if previous, exists := byChannel[capability.ChannelID]; exists && candidates[previous].FilterReason == "" {
				continue
			}
			channel := channels[capability.ChannelID]
			if channel == nil {
				continue
			}
			entitled, exists := entitlements[channel.Id]
			if !exists {
				entitled = true
			}
			current, currentExists := currentByModel[capabilityKey{channel.Id, lookupModel}]
			if currentExists {
				capability = current
			}
			filtered := filterRouteCapability(routeCapabilityFilterInput{
				Capability: capability, SnapshotVersion: capability.SnapshotVersion,
				ChannelStatus: channel.Status, ChannelType: channel.Type,
				AbilityEnabled: abilityAccess[capability.ID].Enabled,
				RequestModel:   input.RequestModel, NormalizedModel: modellab.NormalizeModel(input.RequestModel),
				TokenLimitEnabled: input.TokenModelLimitEnabled, TokenLimit: input.TokenModelLimit,
				RequestPath: input.RequestPath, EndpointType: endpointTypeForTaskRequest(input.RequestPath, input.ExpectedTaskPluginKey),
				Entitled: entitled, RequireSnapshot: true, RequireEndpoint: true,
				Advanced: indexed.Advanced,
			})
			if !currentExists {
				filtered.Reason = RouteFilterSnapshotStale
			}
			candidate := RouteSelectionCandidate{
				ChannelID: channel.Id, RequestModel: capability.RequestModel, ActualModel: capability.ActualModel,
				LabSlug:  capability.LabSlug,
				Priority: channel.GetPriority(), Weight: channel.GetWeight(),
				SnapshotVersion: capability.SnapshotVersion, CatalogVersion: capability.CatalogVersion,
				FilterReason: filtered.Reason, HealthUsable: true, Sticky: channel.Id == input.PreferredChannelID,
			}
			if inManual {
				candidate.Position, candidate.Weight = entry.Position, entry.Weight
				if !entry.Enabled || entry.Source != model.RouteSourcePlatform {
					candidate.FilterReason = "entry_disabled"
				}
			}
			if !TaskPluginCandidateChannelEligible(channel, input.ExpectedTaskPluginKey, input.TaskPluginCandidateKeys) {
				candidate.FilterReason = RouteFilterPathUnsupported
			}
			if input.ResponsesWebSocketRequired && !ChannelSupportsResponsesWebSocket(channel, input.RequestModel) {
				candidate.FilterReason = RouteFilterPathUnsupported
			}
			if input.NativeResponsesRequired && !ChannelSupportsNativeResponses(channel, input.RequestModel) {
				candidate.FilterReason = RouteFilterPathUnsupported
			}
			if input.RequestPath == "/v1/responses/compact" && !ChannelSupportsCompactEndpoint(channel, input.RequestModel) {
				candidate.FilterReason = RouteFilterPathUnsupported
			}
			hasKey := false
			for _, keyIndex := range channel.GetEnabledKeyIndexes() {
				if _, used := input.ExcludedKeyIndexes[channel.Id][keyIndex]; !used {
					hasKey = true
				}
			}
			if !hasKey {
				candidate.FilterReason = RouteFilterKeyUnavailable
			}
			if reason := input.ExcludedChannelIDs[channel.Id]; reason != "" {
				candidate.FilterReason = reason
			}
			// Path, snapshot and key checks above also fail for a retired type;
			// diagnostics must report the cause, not the last check that ran.
			if filtered.Reason == RouteFilterChannelRetired {
				candidate.FilterReason = RouteFilterChannelRetired
			}
			if candidate.FilterReason == "" && input.Requirements != nil && input.ExpectedTaskPluginKey == "" {
				plan, planErr := PlanChannelConversion(*input.Requirements, channel, input.RequestPath, candidate.ActualModel, input.ConversionPolicy)
				if planErr != nil {
					candidate.FilterReason = RoutePreviewFilterConversion
					if len(plan.Diagnostics) == 0 {
						plan.Diagnostics = []types.ConversionDiagnostic{{Code: RoutePreviewFilterConversion, Message: planErr.Error(), Severity: types.ConversionDiagnosticError, From: input.Requirements.Format, To: plan.To}}
					}
				}
				selection.ConversionPlans[channel.Id] = plan
			}
			if previous, exists := byChannel[channel.Id]; exists {
				candidates[previous] = candidate
			} else {
				byChannel[channel.Id] = len(candidates)
				candidates = append(candidates, candidate)
			}
		}
	}
	if err := applyLiveHealth(input.Context, candidates, channels, input.ExcludedKeyIndexes); err != nil {
		return selection, err
	}
	if !input.Preview {
		filterKnownProviderUnavailable(input.Context, candidates)
	}
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput: sourceInput, ManualGroupEnabled: manualEnabled, ManualLoadBalance: loadBalance,
		ManualCandidates: candidates, AutoCandidates: candidates, TopK: MaxRouteCandidateChannels,
		ConfigurationVersion: profile.Version, RequestID: input.RequestID, RequestModel: input.RequestModel,
		DynamicScoringEnabled: selection.Source == RouteSourceAutoLab,
	})
	selection.Decision = result.Decision
	if errors.Is(err, ErrRouteSelectionUnavailable) {
		var diagnostics []types.ConversionDiagnostic
		conversionRejected := false
		for _, candidate := range candidates {
			if candidate.FilterReason == "provider_rate_limited" {
				err = ErrGatewayProviderRateLimit
				break
			}
			if candidate.FilterReason == RoutePreviewFilterConversion {
				conversionRejected = true
				diagnostics = append(diagnostics, selection.ConversionPlans[candidate.ChannelID].Diagnostics...)
			}
		}
		if errors.Is(err, ErrRouteSelectionUnavailable) && conversionRejected {
			err = &types.ConversionLossError{Diagnostics: diagnostics}
		}
	}
	if selection.Source == RouteSourceManual {
		selection.Attempts = manualRouteAttemptCandidates(result, selection.Retry)
	} else {
		selection.Attempts = selectedRouteAttemptCandidates(result)
		selection.Retry = RouteLiveRetryPolicy{Mode: "auto", MaxSameResourceAttempts: DefaultSameChannelAttempts, MaxFailoverAttempts: DefaultFailoverAttempts}
	}
	return selection, err
}
