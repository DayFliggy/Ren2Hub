package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

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
	Model string `json:"model"`
	Path  string `json:"path"`
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
	EntryID         int    `json:"entry_id"`
	ChannelID       int    `json:"channel_id"`
	Position        int    `json:"position"`
	Weight          int    `json:"weight"`
	RequestModel    string `json:"request_model"`
	ActualModel     string `json:"actual_model"`
	LabSlug         string `json:"lab_slug"`
	SnapshotVersion int64  `json:"snapshot_version"`
	CatalogVersion  string `json:"catalog_version"`
	CapabilityState string `json:"capability_state"`
	FilterReason    string `json:"filter_reason,omitempty"`
}

type routePreviewAbility struct {
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
	abilities, err := loadRoutePreviewAbilities(ctx, channelIDs, normalizedModel, userID)
	if err != nil {
		return nil, err
	}
	entitlements, err := loadRoutePreviewEntitlements(ctx, userID, channelIDs)
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
	for _, entry := range activeGroup.Entries {
		item := RoutePreviewEntry{
			EntryID:      entry.ID,
			ChannelID:    entry.ChannelID,
			Position:     entry.Position,
			Weight:       entry.Weight,
			RequestModel: normalizedModel,
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
			Ability:         abilities[entry.ChannelID],
			Entitlement:     entitlements[entry.ChannelID],
			Token:           token,
			RequestModel:    input.Model,
			NormalizedModel: normalizedModel,
			RequestPath:     input.Path,
			EndpointType:    preview.EndpointType,
		})
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

func loadRoutePreviewAbilities(ctx context.Context, channelIDs []int, requestModel string, userID int) (map[int]routePreviewAbility, error) {
	access := make(map[int]routePreviewAbility, len(channelIDs))
	if len(channelIDs) == 0 {
		return access, nil
	}
	var user model.User
	if err := model.DB.WithContext(ctx).Select("id", "group").Where("id = ?", userID).First(&user).Error; err != nil {
		return nil, err
	}
	var rows []model.Ability
	if err := model.DB.WithContext(ctx).Where("channel_id IN ?", channelIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, ability := range rows {
		if modellab.NormalizeModel(ability.Model) != requestModel || !ability.Enabled {
			continue
		}
		value := access[ability.ChannelId]
		value.Enabled = true
		if ability.Group == user.Group || IsUserSelectableGroup(user.Group, ability.Group) {
			value.Allowed = true
		}
		access[ability.ChannelId] = value
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

type routePreviewFilterInput struct {
	Profile         model.UserRouteProfile
	Group           model.UserRouteGroup
	Entry           model.UserRouteEntry
	Channel         model.Channel
	ChannelExists   bool
	Capability      model.ChannelModelCapability
	SnapshotVersion int64
	Ability         routePreviewAbility
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
		PriceEligible:   true,
		SecurityAllowed: true,
		RequireSnapshot: true,
		RequireEndpoint: true,
	})
	return result.Reason
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
