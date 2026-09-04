package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/modellab"
)

const (
	RoutePreviewSelectionOrdered  = "ordered"
	RoutePreviewSelectionWeighted = "weighted"

	RoutePreviewFilterActiveGroupMissing = "active_group_missing"
	RoutePreviewFilterProfileDisabled    = "profile_disabled"
	RoutePreviewFilterGroupDisabled      = "group_disabled"
	RoutePreviewFilterEntryDisabled      = "entry_disabled"
	RoutePreviewFilterChannelMissing     = "channel_not_found"
	RoutePreviewFilterSourceUnsupported  = "source_unsupported"
)

// RouteProfilePreviewInput is intentionally limited to request facts that are
// needed for a static manual-route preview. It never selects or reserves a
// live channel.
type RouteProfilePreviewInput struct {
	Model          string `json:"model"`
	Path           string `json:"path"`
	EffectiveGroup string `json:"-"`
}

type RouteProfilePreview struct {
	ProfileID              int                   `json:"profile_id"`
	ProfileVersion         int64                 `json:"profile_version"`
	RequestModel           string                `json:"request_model"`
	NormalizedModel        string                `json:"normalized_model"`
	Path                   string                `json:"path"`
	EndpointType           string                `json:"endpoint_type"`
	ActiveGroup            *model.UserRouteGroup `json:"active_group,omitempty"`
	Policy                 *model.RoutePolicy    `json:"policy,omitempty"`
	Entries                []RoutePreviewEntry   `json:"entries"`
	CandidateChannelIDs    []int                 `json:"candidate_channel_ids"`
	SelectionMode          string                `json:"selection_mode"`
	PreferredChannelID     int                   `json:"preferred_channel_id,omitempty"`
	FilterReasonCounts     map[string]int        `json:"filter_reason_counts"`
	HasMixed               bool                  `json:"has_mixed"`
	RuntimeRecheckRequired bool                  `json:"runtime_recheck_required"`
	RuntimeRecheckReasons  []string              `json:"runtime_recheck_reasons"`
	LiveSelection          bool                  `json:"live_selection"`
}

type RoutePreviewEntry struct {
	EntryID         int                       `json:"entry_id"`
	ChannelID       int                       `json:"channel_id"`
	Position        int                       `json:"position"`
	Weight          int                       `json:"weight"`
	RequestModel    string                    `json:"request_model"`
	ActualModel     string                    `json:"actual_model"`
	LabSlug         string                    `json:"lab_slug"`
	SnapshotVersion int64                     `json:"snapshot_version"`
	CatalogVersion  string                    `json:"catalog_version"`
	CapabilityState string                    `json:"capability_state"`
	Health          RoutePreviewHealthSummary `json:"health"`
	FilterReason    string                    `json:"filter_reason,omitempty"`
}

// RoutePreviewHealthSummary is the non-sensitive aggregate health state for
// one channel and canonical request model. Key-scoped observations remain
// internal and are intentionally not exposed through user routing APIs.
type RoutePreviewHealthSummary struct {
	State               string `json:"state"`
	FailureCount        int    `json:"failure_count"`
	CooldownUntil       int64  `json:"cooldown_until"`
	HealthEpoch         int64  `json:"health_epoch"`
	LastLatencyMS       int64  `json:"last_latency_ms"`
	FirstTokenLatencyMS int64  `json:"first_token_latency_ms"`
	UpdatedAt           int64  `json:"updated_at"`
}

type RouteCapabilityUserAccess struct {
	Enabled bool
	Allowed bool
}

// PreviewUserRouteProfile evaluates only the active manual group. It is a
// configuration preview and deliberately has no relay, billing, retry, or
// context side effects.
func PreviewUserRouteProfile(ctx context.Context, userID, profileID int, input RouteProfilePreviewInput) (*RouteProfilePreview, error) {
	input.Model = strings.TrimSpace(input.Model)
	input.Path = strings.TrimSpace(input.Path)
	normalizedModel := modellab.NormalizeModel(input.Model)
	if normalizedModel == "" || input.Path == "" {
		return nil, fmt.Errorf("%w: model and path are required", ErrRouteProfileValidation)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	view, err := GetUserRouteProfile(userID, profileID)
	if err != nil {
		return nil, err
	}
	if view.Profile.Mode != model.RouteModeManual {
		return nil, fmt.Errorf("%w: manual route profile is required", ErrRouteProfileValidation)
	}

	preview := &RouteProfilePreview{
		ProfileID:              view.Profile.ID,
		ProfileVersion:         view.Profile.Version,
		RequestModel:           input.Model,
		NormalizedModel:        normalizedModel,
		Path:                   input.Path,
		EndpointType:           endpointTypeForRequestPath(input.Path),
		Entries:                make([]RoutePreviewEntry, 0),
		CandidateChannelIDs:    make([]int, 0),
		SelectionMode:          RoutePreviewSelectionOrdered,
		FilterReasonCounts:     make(map[string]int),
		RuntimeRecheckRequired: true,
		RuntimeRecheckReasons:  []string{"price_qualification", "quota_qualification", "security_policy"},
		LiveSelection:          false,
	}
	activeGroup := findActiveRouteGroup(view)
	if activeGroup == nil {
		preview.FilterReasonCounts[RoutePreviewFilterActiveGroupMissing]++
		return preview, nil
	}
	group := activeGroup.Group
	policy := activeGroup.Policy
	preview.ActiveGroup = &group
	preview.Policy = &policy

	channelIDs := routeGroupChannelIDs(activeGroup.Entries)
	channels, err := loadRoutePreviewChannels(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	capabilities, err := model.FindActiveChannelCapabilities(ctx, channelIDs, normalizedModel, "")
	if err != nil {
		return nil, err
	}
	activeSnapshots, err := loadRoutePreviewSnapshots(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	abilityAccess, err := FindUserRouteCapabilityAccess(ctx, userID, capabilities)
	if err != nil {
		return nil, err
	}
	entitlements, err := loadRoutePreviewEntitlements(ctx, userID, channelIDs)
	if err != nil {
		return nil, err
	}
	healthByChannel, err := loadRoutePreviewHealth(ctx, channelIDs, normalizedModel)
	if err != nil {
		return nil, err
	}
	capabilityByChannel := make(map[int]model.ChannelModelCapability, len(capabilities))
	for _, capability := range capabilities {
		capabilityByChannel[capability.ChannelID] = capability
	}

	var token model.Token
	if err := model.DB.WithContext(ctx).Where("id = ? AND user_id = ?", view.Profile.TokenID, userID).First(&token).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	for _, entry := range activeGroup.Entries {
		item := RoutePreviewEntry{
			EntryID:         entry.ID,
			ChannelID:       entry.ChannelID,
			Position:        entry.Position,
			Weight:          entry.Weight,
			RequestModel:    normalizedModel,
			CapabilityState: model.RouteCapabilityStateUnresolved,
			Health:          defaultRoutePreviewHealthSummary(),
		}
		health := healthByChannel[entry.ChannelID]
		if health.ID != 0 {
			item.Health = routePreviewHealthSummary(health)
		}
		activeSnapshot := activeSnapshots[entry.ChannelID]
		item.FilterReason = routePreviewFilterReason(routePreviewFilterInput{
			Profile:         view.Profile,
			Group:           group,
			Entry:           entry,
			Channel:         channels[entry.ChannelID],
			ChannelExists:   channels[entry.ChannelID].Id > 0,
			Capability:      capabilityByChannel[entry.ChannelID],
			SnapshotVersion: activeSnapshot.ActiveVersion,
			Ability:         abilityAccess[capabilityByChannel[entry.ChannelID].ID],
			Entitlement:     entitlements[entry.ChannelID],
			Token:           token,
			RequestModel:    input.Model,
			NormalizedModel: normalizedModel,
			RequestPath:     input.Path,
			EndpointType:    preview.EndpointType,
		})
		if item.FilterReason == "" && !RouteHealthReadOnlyUsable(health, now) {
			item.FilterReason = RouteCandidateFilterHealthUnavailable
		}
		if item.FilterReason == "" {
			channel := channels[entry.ChannelID]
			hasAvailableKey, keyErr := RouteChannelHasReadOnlyAvailableKey(ctx, &channel, normalizedModel, now)
			if keyErr != nil {
				return nil, keyErr
			}
			if !hasAvailableKey {
				item.FilterReason = RouteFilterKeyUnavailable
			}
		}
		item.SnapshotVersion = activeSnapshot.ActiveVersion
		item.CatalogVersion = activeSnapshot.CatalogVersion
		if capability, ok := capabilityByChannel[entry.ChannelID]; ok {
			preview.HasMixed = preview.HasMixed || capability.IsMixed
			item.ActualModel = capability.ActualModel
			item.LabSlug = capability.LabSlug
			item.SnapshotVersion = capability.SnapshotVersion
			item.CatalogVersion = capability.CatalogVersion
			item.CapabilityState = capability.State
		}
		if item.FilterReason != "" {
			preview.FilterReasonCounts[item.FilterReason]++
		}
		preview.Entries = append(preview.Entries, item)
	}

	sortRoutePreviewEntries(preview.Entries, policy.LoadBalance)
	eligible := make([]RoutePreviewEntry, 0, len(preview.Entries))
	for _, entry := range preview.Entries {
		if entry.FilterReason == "" {
			eligible = append(eligible, entry)
			preview.CandidateChannelIDs = append(preview.CandidateChannelIDs, entry.ChannelID)
		}
	}
	if len(eligible) == 0 {
		return preview, nil
	}
	firstPosition := eligible[0].Position
	topLayer := make([]RoutePreviewEntry, 0, len(eligible))
	for _, entry := range eligible {
		if entry.Position != firstPosition {
			break
		}
		topLayer = append(topLayer, entry)
	}
	if policy.LoadBalance && len(topLayer) > 1 {
		preview.SelectionMode = RoutePreviewSelectionWeighted
	}
	preview.PreferredChannelID = topLayer[0].ChannelID
	return preview, nil
}

func findActiveRouteGroup(view *RouteProfileView) *RouteGroupView {
	if view == nil || view.Profile.ActiveGroupID == nil {
		return nil
	}
	for index := range view.Groups {
		if view.Groups[index].Group.ID == *view.Profile.ActiveGroupID {
			return &view.Groups[index]
		}
	}
	return nil
}

func routeGroupChannelIDs(entries []model.UserRouteEntry) []int {
	seen := make(map[int]struct{}, len(entries))
	channelIDs := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.ChannelID <= 0 {
			continue
		}
		if _, exists := seen[entry.ChannelID]; exists {
			continue
		}
		seen[entry.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, entry.ChannelID)
	}
	sort.Ints(channelIDs)
	return channelIDs
}

func loadRoutePreviewChannels(ctx context.Context, channelIDs []int) (map[int]model.Channel, error) {
	channels := make(map[int]model.Channel, len(channelIDs))
	if len(channelIDs) == 0 {
		return channels, nil
	}
	var rows []model.Channel
	if err := model.DB.WithContext(ctx).Where("id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, channel := range rows {
		channels[channel.Id] = channel
	}
	return channels, nil
}

type routePreviewSnapshot struct {
	ActiveVersion  int64
	CatalogVersion string
}

func loadRoutePreviewSnapshots(ctx context.Context, channelIDs []int) (map[int]routePreviewSnapshot, error) {
	snapshots := make(map[int]routePreviewSnapshot, len(channelIDs))
	if len(channelIDs) == 0 {
		return snapshots, nil
	}
	rows, err := model.FindActiveChannelCapabilitySnapshots(ctx, channelIDs)
	if err != nil {
		return nil, err
	}
	for _, snapshot := range rows {
		snapshots[snapshot.ChannelID] = routePreviewSnapshot{
			ActiveVersion:  snapshot.ActiveVersion,
			CatalogVersion: snapshot.CatalogVersion,
		}
	}
	return snapshots, nil
}

// FindUserRouteCapabilityAccess applies current Ability.enabled and user-group
// authorization to immutable capability rows. The snapshot describes what a
// channel could serve when refreshed; the live Ability table remains the
// authority for whether this user may see or select that request model.
func FindUserRouteCapabilityAccess(ctx context.Context, userID int, capabilities []model.ChannelModelCapability) (map[int]RouteCapabilityUserAccess, error) {
	access := make(map[int]RouteCapabilityUserAccess, len(capabilities))
	if len(capabilities) == 0 {
		return access, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "group").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	capabilityIDsByChannelModel := make(map[int]map[string][]int, len(capabilities))
	channelIDs := make([]int, 0, len(capabilities))
	seenChannels := make(map[int]struct{}, len(capabilities))
	for _, capability := range capabilities {
		requestModel := modellab.NormalizeModel(capability.RequestModel)
		if capability.ID <= 0 || capability.ChannelID <= 0 || requestModel == "" {
			continue
		}
		if _, exists := capabilityIDsByChannelModel[capability.ChannelID]; !exists {
			capabilityIDsByChannelModel[capability.ChannelID] = make(map[string][]int)
		}
		capabilityIDsByChannelModel[capability.ChannelID][requestModel] = append(capabilityIDsByChannelModel[capability.ChannelID][requestModel], capability.ID)
		if _, exists := seenChannels[capability.ChannelID]; !exists {
			seenChannels[capability.ChannelID] = struct{}{}
			channelIDs = append(channelIDs, capability.ChannelID)
		}
	}
	var rows []model.Ability
	if err := model.DB.WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, ability := range rows {
		capabilityIDs := capabilityIDsByChannelModel[ability.ChannelId][modellab.NormalizeModel(ability.Model)]
		if len(capabilityIDs) == 0 || !ability.Enabled {
			continue
		}
		for _, capabilityID := range capabilityIDs {
			value := access[capabilityID]
			value.Enabled = true
			if ability.Group == user.Group || IsUserSelectableGroup(user.Group, ability.Group) {
				value.Allowed = true
			}
			access[capabilityID] = value
		}
	}
	return access, nil
}

func loadRoutePreviewEntitlements(ctx context.Context, userID int, channelIDs []int) (map[int]model.UserChannelEntitlement, error) {
	entitlements := make(map[int]model.UserChannelEntitlement, len(channelIDs))
	if len(channelIDs) == 0 {
		return entitlements, nil
	}
	var rows []model.UserChannelEntitlement
	if err := model.DB.WithContext(ctx).Where("user_id = ? AND channel_id IN ? AND source = ?", userID, channelIDs, model.RouteSourcePlatform).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, entitlement := range rows {
		entitlements[entitlement.ChannelID] = entitlement
	}
	return entitlements, nil
}

func loadRoutePreviewHealth(ctx context.Context, channelIDs []int, canonicalModel string) (map[int]model.ChannelHealth, error) {
	healthByChannel := make(map[int]model.ChannelHealth, len(channelIDs))
	if len(channelIDs) == 0 {
		return healthByChannel, nil
	}
	var rows []model.ChannelHealth
	if err := model.DB.WithContext(ctx).
		Where("channel_id IN ? AND model = ? AND key_scope = ?", channelIDs, canonicalModel, "").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, health := range rows {
		healthByChannel[health.ChannelID] = health
	}
	return healthByChannel, nil
}

func defaultRoutePreviewHealthSummary() RoutePreviewHealthSummary {
	return RoutePreviewHealthSummary{
		State:       model.RouteHealthStateClosed,
		HealthEpoch: 1,
	}
}

func routePreviewHealthSummary(health model.ChannelHealth) RoutePreviewHealthSummary {
	summary := RoutePreviewHealthSummary{
		State:               health.State,
		FailureCount:        health.FailureCount,
		CooldownUntil:       health.CooldownUntil,
		HealthEpoch:         health.HealthEpoch,
		LastLatencyMS:       health.LastLatencyMS,
		FirstTokenLatencyMS: health.FirstTokenLatencyMS,
		UpdatedAt:           health.UpdatedAt,
	}
	if summary.State == "" {
		summary.State = model.RouteHealthStateClosed
	}
	if summary.HealthEpoch <= 0 {
		summary.HealthEpoch = 1
	}
	return summary
}

// routePreviewHealthUsable is deliberately read-only. A live request is the
// only path allowed to claim a half-open probe, while Preview must show the
// same unavailable state without changing health ownership.
func routePreviewHealthUsable(health RoutePreviewHealthSummary) bool {
	return health.State == "" || health.State == model.RouteHealthStateClosed
}

type routePreviewFilterInput struct {
	Profile         model.UserRouteProfile
	Group           model.UserRouteGroup
	Entry           model.UserRouteEntry
	Channel         model.Channel
	ChannelExists   bool
	Capability      model.ChannelModelCapability
	SnapshotVersion int64
	Ability         RouteCapabilityUserAccess
	Entitlement     model.UserChannelEntitlement
	Token           model.Token
	RequestModel    string
	NormalizedModel string
	RequestPath     string
	EndpointType    string
}

func routePreviewFilterReason(input routePreviewFilterInput) string {
	if input.Profile.Status != model.RouteProfileStatusEnabled {
		return RoutePreviewFilterProfileDisabled
	}
	if !input.Group.Enabled {
		return RoutePreviewFilterGroupDisabled
	}
	if !input.Entry.Enabled {
		return RoutePreviewFilterEntryDisabled
	}
	if input.Entry.Source != model.RouteSourcePlatform {
		return RoutePreviewFilterSourceUnsupported
	}
	if !input.ChannelExists {
		return RoutePreviewFilterChannelMissing
	}
	if input.Token.Status != common.TokenStatusEnabled ||
		(input.Token.ExpiredTime != -1 && input.Token.ExpiredTime <= common.GetTimestamp()) {
		return ShadowFilterTokenForbidden
	}
	entitled := input.Entitlement.ID == 0 || entitlementIsActive(input.Entitlement)
	result := filterRouteCapability(routeCapabilityFilterInput{
		Capability:      input.Capability,
		SnapshotVersion: input.SnapshotVersion,
		ChannelStatus:   input.Channel.Status,
		ChannelType:     input.Channel.Type,
		AbilityEnabled:  input.Ability.Enabled,
		AbilityAllowed:  input.Ability.Allowed,
		UserGroup:       "",
		Token:           input.Token,
		RequestModel:    input.RequestModel,
		NormalizedModel: input.NormalizedModel,
		RequestPath:     input.RequestPath,
		EndpointType:    input.EndpointType,
		Entitled:        entitled,
		// Price, quota, and request-body security facts are not available to a
		// configuration-only preview. The response exposes a runtime recheck
		// requirement instead of treating this entry as fully qualified.
		PriceEligibilityKnown:    false,
		SecurityEligibilityKnown: false,
		RequireSnapshot:          true,
		RequireEndpoint:          true,
	})
	return result.Reason
}

func routeTokenUsable(token model.Token) bool {
	return token.Status == common.TokenStatusEnabled &&
		(token.ExpiredTime == -1 || token.ExpiredTime > common.GetTimestamp())
}

func entitlementIsActive(entitlement model.UserChannelEntitlement) bool {
	return entitlement.Status == model.RouteEntitlementStatusEnabled &&
		entitlement.RevokedAt == 0 &&
		(entitlement.ExpiresAt == 0 || entitlement.ExpiresAt > time.Now().Unix())
}

func sortRoutePreviewEntries(entries []RoutePreviewEntry, loadBalance bool) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.Position != right.Position {
			return left.Position < right.Position
		}
		if loadBalance && left.Weight != right.Weight {
			return left.Weight > right.Weight
		}
		return left.ChannelID < right.ChannelID
	})
}
