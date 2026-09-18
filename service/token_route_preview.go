package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
	"github.com/QuantumNous/new-api/relaykit/types"
	"gorm.io/gorm"
)

func PreviewUserRouteProfile(ctx context.Context, userID, profileID int, input RouteProfilePreviewInput) (*RouteProfilePreview, error) {
	view, err := GetUserRouteProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	return PreviewUserTokenRoute(ctx, userID, view.Profile.TokenID, input)
}

// PreviewUserTokenRoute uses the dispatch selector without reserving resources
// or writing provider state. Its generated ID samples weighted order once.
func PreviewUserTokenRoute(ctx context.Context, userID, tokenID int, input RouteProfilePreviewInput) (*RouteProfilePreview, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input.Model, input.Path = strings.TrimSpace(input.Model), strings.TrimSpace(input.Path)
	if userID <= 0 || tokenID <= 0 || model.DB == nil {
		return nil, ErrRouteProfileNotFound
	}
	var token model.Token
	if err := model.DB.WithContext(ctx).Where("id = ? AND user_id = ?", tokenID, userID).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRouteProfileNotFound
		}
		return nil, err
	}
	if input.Model == "" || input.Path == "" {
		return nil, fmt.Errorf("%w: model and path are required", ErrRouteProfileValidation)
	}
	requirements, err := analyzeRoutePreviewRequest(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrRouteProfileValidation, err)
	}
	policy := types.ConversionLossPolicy(token.ConversionPolicy)
	if policy == "" {
		policy = types.ConversionLossPolicySafe
	}
	if err := policy.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid conversion policy", ErrRouteProfileValidation)
	}
	profile := model.UserRouteProfile{Mode: model.RouteModeAutoLab}
	if err := model.DB.WithContext(ctx).Where("user_id = ? AND token_id = ?", userID, tokenID).First(&profile).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	preview := &RouteProfilePreview{
		ProfileID: profile.ID, ProfileVersion: profile.Version,
		RequestModel: input.Model, NormalizedModel: modellab.NormalizeModel(input.Model),
		Path: input.Path, EndpointType: endpointTypeForRequestPath(input.Path),
		Entries: []RoutePreviewEntry{}, CandidateChannelIDs: []int{},
		SelectionMode: RoutePreviewSelectionWeighted, FilterReasonCounts: map[string]int{},
		RuntimeRecheckRequired: true, RuntimeRecheckReasons: []string{"price_qualification", "quota_qualification", "security_policy"},
		RequestRequirements: requirements, ConversionPolicy: policy,
	}
	if profile.Mode == model.RouteModeManual {
		preview, err = loadManualRoutePreviewContext(ctx, userID, profile.ID, input, requirements)
		if err != nil {
			return nil, err
		}
	}
	preview.TokenID, preview.Mode = tokenID, profile.Mode
	preview.PreviewID, preview.GeneratedAt = common.NewRequestId(), time.Now().UnixMilli()
	preview.RuntimeRecheckReasons = append(preview.RuntimeRecheckReasons, "admission_capacity", "provider_rate_limit", "configuration_version", "affinity")
	if profile.ID > 0 && profile.Status != model.RouteProfileStatusEnabled && profile.Mode != model.RouteModeManual {
		preview.FilterReasonCounts[RoutePreviewFilterProfileDisabled]++
		return preview, nil
	}
	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "status").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	if user.Status != common.UserStatusEnabled {
		for index := range preview.Entries {
			preview.Entries[index].FilterReason = RouteFilterTokenForbidden
		}
		preview.FilterReasonCounts = map[string]int{RouteFilterTokenForbidden: max(len(preview.Entries), 1)}
		return preview, nil
	}
	if len(preview.Entries) == 0 && len(preview.FilterReasonCounts) > 0 {
		return preview, nil
	}
	if requirements.NativeRequired && profile.Mode != model.RouteModeManual {
		preview.RuntimeRecheckReasons = append(preview.RuntimeRecheckReasons, "state_ownership")
	}
	selection, selectionErr := SelectLiveTokenRoute(LiveRouteRequest{
		Context: ctx, Preview: true, CapabilityEnabled: true, RequestID: preview.PreviewID,
		UserID: userID, TokenID: tokenID, RequestModel: input.Model, RequestPath: input.Path,
		TokenModelLimitEnabled: token.ModelLimitsEnabled, TokenModelLimit: token.GetModelLimitsMap(),
		Requirements: &requirements, ConversionPolicy: policy,
	})
	var conversionErr *types.ConversionLossError
	if selectionErr != nil && !errors.Is(selectionErr, ErrRouteSelectionUnavailable) && !errors.As(selectionErr, &conversionErr) {
		return nil, selectionErr
	}
	ids := make([]int, 0, len(selection.Decision.Candidates))
	for _, candidate := range selection.Decision.Candidates {
		ids = append(ids, candidate.ChannelID)
	}
	capabilities, err := model.FindActiveChannelCapabilities(ctx, ids, "", "")
	if err != nil {
		return nil, err
	}
	type candidateKey struct {
		channelID int
		model     string
	}
	capabilityByChannel := make(map[candidateKey]model.ChannelModelCapability, len(capabilities))
	for _, capability := range capabilities {
		capabilityByChannel[candidateKey{capability.ChannelID, capability.RequestModel}] = capability
	}
	health := make(map[candidateKey]model.ChannelHealth)
	seenModels := make(map[string]bool)
	for _, candidate := range selection.Decision.Candidates {
		if seenModels[candidate.RequestModel] {
			continue
		}
		seenModels[candidate.RequestModel] = true
		rows, err := loadRoutePreviewHealth(ctx, ids, candidate.RequestModel)
		if err != nil {
			return nil, err
		}
		for channelID, row := range rows {
			health[candidateKey{channelID, candidate.RequestModel}] = row
		}
	}
	entryIndexes := make(map[int]int, len(preview.Entries))
	for index, entry := range preview.Entries {
		entryIndexes[entry.ChannelID] = index
	}
	for _, candidate := range selection.Decision.Candidates {
		key := candidateKey{candidate.ChannelID, candidate.RequestModel}
		capability := capabilityByChannel[key]
		if capability.State == "" {
			capability.State = model.RouteCapabilityStateUnresolved
		}
		index, exists := entryIndexes[candidate.ChannelID]
		if !exists {
			index = len(preview.Entries)
			preview.Entries = append(preview.Entries, RoutePreviewEntry{
				ChannelID: candidate.ChannelID, Position: candidate.Position, Weight: candidate.Weight,
				Diagnostics: []types.ConversionDiagnostic{},
			})
		}
		entry := &preview.Entries[index]
		entry.RequestModel, entry.ActualModel = candidate.RequestModel, capability.ActualModel
		entry.LabSlug, entry.CapabilityState = capability.LabSlug, capability.State
		entry.SnapshotVersion, entry.CatalogVersion = candidate.SnapshotVersion, candidate.CatalogVersion
		entry.Health = routePreviewHealthSummary(health[key])
		preview.HasMixed = preview.HasMixed || capability.IsMixed
		entry.Priority, entry.Score = candidate.Priority, candidate.Score
		entry.FilterReason = candidate.FilterReason
		if plan, found := selection.ConversionPlans[candidate.ChannelID]; found {
			entry.ConversionPlan = &plan
			entry.Diagnostics = append([]types.ConversionDiagnostic{}, plan.Diagnostics...)
		}
		if !routeTokenUsable(token) {
			entry.FilterReason = RouteFilterTokenForbidden
		}
	}
	preview.CandidateChannelIDs = []int{}
	preview.PreferredChannelID = 0
	preview.FilterReasonCounts = map[string]int{}
	eligible := make(map[int]bool, len(preview.Entries))
	for _, entry := range preview.Entries {
		if entry.FilterReason != "" {
			preview.FilterReasonCounts[entry.FilterReason]++
		} else {
			eligible[entry.ChannelID] = true
		}
	}
	seen := make(map[int]bool)
	for _, candidate := range selection.Attempts {
		if eligible[candidate.ChannelID] && !seen[candidate.ChannelID] {
			preview.CandidateChannelIDs = append(preview.CandidateChannelIDs, candidate.ChannelID)
			seen[candidate.ChannelID] = true
		}
	}
	if len(preview.CandidateChannelIDs) > 0 {
		preview.PreferredChannelID = preview.CandidateChannelIDs[0]
	}
	return preview, nil
}
