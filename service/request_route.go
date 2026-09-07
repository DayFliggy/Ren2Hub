package service

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"gorm.io/gorm"
)

// SelectLiveTokenRoute is the single request planner for every relay protocol.
// A token without a private profile uses automatic routing; missing candidates
// or infrastructure never invoke another selector.
func SelectLiveTokenRoute(input LiveRouteRequest) (LiveRouteSelection, error) {
	selection := LiveRouteSelection{Source: RouteSourceUnavailable, CompactStage: input.CompactStage, SpecificChannelID: input.SpecificChannelID}
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
	if input.CompactStage != relaycommon.CompactAttemptNone {
		base := ratio_setting.CompactBaseModelName(input.RequestModel)
		models = []string{modellab.NormalizeModel(base)}
		if input.CompactStage == relaycommon.CompactAttemptExact {
			models = append([]string{modellab.NormalizeModel(ratio_setting.WithCompactModelSuffix(base))}, models...)
		}
	}
	entitlements, err := liveRouteEntitlements(input.Context, input.UserID)
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
			channel, err := model.GetChannelById(capability.ChannelID, true)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				return selection, err
			}
			entitled, exists := entitlements[channel.Id]
			if !exists {
				entitled = true
			}
			filtered := filterRouteCapability(routeCapabilityFilterInput{
				Capability: capability, SnapshotVersion: capability.SnapshotVersion,
				ChannelStatus: channel.Status, ChannelType: channel.Type,
				AbilityEnabled: len(indexed.AbilityGroups) > 0,
				RequestModel:   input.RequestModel, NormalizedModel: modellab.NormalizeModel(input.RequestModel),
				TokenLimitEnabled: input.TokenModelLimitEnabled, TokenLimit: input.TokenModelLimit,
				RequestPath: input.RequestPath, EndpointType: endpointTypeForRequestPath(input.RequestPath),
				Entitled: entitled, RequireSnapshot: true, RequireEndpoint: true,
				Advanced: indexed.Advanced,
			})
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
			if input.NativeResponsesRequired && !ChannelSupportsNativeResponses(channel, input.RequestModel) {
				candidate.FilterReason = RouteFilterPathUnsupported
			}
			if input.CompactStage != relaycommon.CompactAttemptNone {
				base := ratio_setting.CompactBaseModelName(input.RequestModel)
				abilities := make(map[string]bool)
				var rows []model.Ability
				if err := model.DB.WithContext(input.Context).Where(&model.Ability{ChannelId: channel.Id, Enabled: true}).Find(&rows).Error; err != nil {
					return selection, err
				}
				for _, row := range rows {
					abilities[row.Model] = true
				}
				if !compactChannelSupportsStage(channel, abilities, base, ratio_setting.WithCompactModelSuffix(base), input.CompactStage) {
					candidate.FilterReason = RouteFilterPathUnsupported
				}
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
			if previous, exists := byChannel[channel.Id]; exists {
				candidates[previous] = candidate
			} else {
				byChannel[channel.Id] = len(candidates)
				candidates = append(candidates, candidate)
			}
		}
	}
	if err := applyLiveHealth(input.Context, candidates); err != nil {
		return selection, err
	}
	result, err := SelectTokenRoute(RouteSelectionInput{
		SourceInput: sourceInput, ManualGroupEnabled: manualEnabled, ManualLoadBalance: loadBalance,
		ManualCandidates: candidates, AutoCandidates: candidates, TopK: 3,
		ConfigurationVersion: profile.Version, RequestID: input.RequestID, RequestModel: input.RequestModel,
		DynamicScoringEnabled: selection.Source == RouteSourceAutoLab,
	})
	selection.Decision = result.Decision
	if selection.Source == RouteSourceManual {
		selection.Attempts = manualRouteAttemptCandidates(result, selection.Retry)
	} else {
		selection.Attempts = selectedRouteAttemptCandidates(result)
		selection.Retry = RouteLiveRetryPolicy{Mode: "auto", MaxSameResourceAttempts: DefaultSameChannelAttempts, MaxFailoverAttempts: DefaultFailoverAttempts}
	}
	return selection, err
}
