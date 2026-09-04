package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectLiveTokenRouteManualUsesOnlyActiveGroup(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-manual",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceManual, selection.Source)
	assert.Equal(t, channelID, selection.Decision.SelectedChannelID)
	assert.Equal(t, created.Profile.Version, selection.Decision.ConfigurationVersion)
}

func TestSelectLiveTokenRouteManualUsesEffectiveTokenGroup(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	originalSpecialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup.ReadAll()
	t.Cleanup(func() {
		specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecialGroups)
	})
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	specialGroups.Clear()
	specialGroups.Set("vip", map[string]string{"-:default": ""})
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true, Position: 0,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("group", "vip").Error)
	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-token-group",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "vip",
	})
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
	assert.Equal(t, RouteSourceManual, selection.Source)
	assert.Zero(t, selection.Decision.SelectedChannelID)
	assert.Equal(t, created.Profile.Version, selection.Decision.ConfigurationVersion)
	require.Len(t, selection.Decision.Candidates, 1)
	assert.Equal(t, ShadowFilterGroupForbidden, selection.Decision.Candidates[0].FilterReason)
}

func TestLiveRouteOnlyAcceptsConcreteTokenGroupsAndRelayPaths(t *testing.T) {
	assert.True(t, RouteLiveTokenGroupSupported("default"))
	assert.False(t, RouteLiveTokenGroupSupported(""))
	assert.False(t, RouteLiveTokenGroupSupported("auto"))
	assert.True(t, RouteLiveRequestSupported("/v1/chat/completions"))
	assert.True(t, RouteLiveRequestSupported("/v1/messages"))
	assert.False(t, RouteLiveRequestSupported("/mj/submit/imagine"))
	assert.False(t, RouteLiveRequestSupported("/suno/submit/generate"))
}

func TestSelectLiveTokenRouteAutoLabUsesActiveIndexAndCurrentChannelStatus(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	channel := model.Channel{}
	require.NoError(t, db.First(&channel, "id = ?", channelID).Error)
	abilityGroups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	capability := model.ChannelModelCapability{
		ChannelID: channelID, SnapshotVersion: 1, RequestModel: "gpt-test", ActualModel: "gpt-test",
		LabSlug: "openai", Source: "canonical", State: model.RouteCapabilityStateEligible,
		AbilityGroups: string(abilityGroups), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, EndpointTypes: `[
  "openai"
]`, Priority: 20, Weight: 10,
	}
	resetRouteCapabilityIndexForTest()
	routeCapabilityIndex.Store(&capabilityIndex{
		ByRequestModel: map[string][]indexedCapability{"gpt-test": {{
			Capability: capability, ChannelStatus: common.ChannelStatusEnabled,
			Priority: 20, Weight: 10, AbilityGroups: []string{"default"}, ChannelType: constant.ChannelTypeOpenAI,
		}}},
	})
	t.Cleanup(resetRouteCapabilityIndexForTest)
	_, err = CreateUserRouteProfile(RouteProfileInput{UserID: userID, TokenID: tokenID, Mode: model.RouteModeAutoLab})
	require.NoError(t, err)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-auto",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceAutoLab, selection.Source)
	assert.Equal(t, channelID, selection.Decision.SelectedChannelID)

	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", channelID).Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, err = SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-auto-disabled",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default",
	})
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
}

func TestSelectLiveTokenRouteMissingProfileKeepsLegacy(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, _ := seedRouteProfileFixture(t, db)
	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, UserID: userID, TokenID: tokenID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceLegacy, selection.Source)
	assert.Empty(t, selection.Decision.SelectedChannelID)
}

func TestSelectLiveTokenRouteDisabledSkipsProfileLookup(t *testing.T) {
	originalDB := model.DB
	model.DB = nil
	t.Cleanup(func() { model.DB = originalDB })

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		CapabilityEnabled: false,
		UserID:            1,
		TokenID:           2,
		RequestModel:      "gpt-test",
		RequestPath:       "/v1/chat/completions",
	})
	require.NoError(t, err)
	assert.Equal(t, RouteSourceLegacy, selection.Source)
	assert.Empty(t, selection.Decision.SelectedChannelID)
}

func TestSelectLiveTokenRouteFiltersOpenChannelModelHealth(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "health", Enabled: true, Entries: []RouteEntryInput{{
			ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
		}}}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: "", State: model.RouteHealthStateOpen,
		FailureCount: 3, CooldownUntil: time.Now().Add(time.Hour).Unix(), HealthEpoch: 2,
	}).Error)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-health",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
	})
	assert.Equal(t, RouteSourceManual, selection.Source)
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
	assert.Equal(t, created.Profile.Version, selection.Decision.ConfigurationVersion)
	assert.Equal(t, RouteCandidateFilterHealthUnavailable, selection.Decision.Candidates[0].FilterReason)
}

func TestSelectLiveTokenRouteFiltersChannelWhenEveryKeyIsIsolated(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{Name: "key-health", Enabled: true, Entries: []RouteEntryInput{{
			ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true,
		}}}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", KeyScope: RouteKeyScope("routing-channel-key"), State: model.RouteHealthStateOpen,
		FailureCount: 1, CooldownUntil: time.Now().Add(time.Hour).Unix(), HealthEpoch: 2,
	}).Error)

	selection, err := SelectLiveTokenRoute(LiveRouteRequest{
		Context: context.Background(), CapabilityEnabled: true, RequestID: "request-key-health",
		UserID: userID, TokenID: tokenID, RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
	})
	assert.ErrorIs(t, err, ErrRouteSelectionUnavailable)
	require.Len(t, selection.Decision.Candidates, 1)
	assert.Equal(t, RouteFilterKeyUnavailable, selection.Decision.Candidates[0].FilterReason)
}

func TestRecheckLiveRouteCandidateRejectsAbilityDisabledAfterSelection(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channelID).Update("enabled", false).Error)

	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default", ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
	})
	assert.ErrorIs(t, err, ErrLiveRouteCandidateInvalid)
	assert.Equal(t, ShadowFilterAbilityDisabled, LiveRouteQualificationReason(err))
}

func TestRecheckLiveRouteCandidateRejectsChangedGroupAndEntry(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channelID).Update("group", "__no_access__").Error)
	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup:               "default",
		ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
		ExpectedProfileVersion: created.Profile.Version,
	})
	assert.Equal(t, ShadowFilterGroupForbidden, LiveRouteQualificationReason(err))

	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channelID).Update("group", "default").Error)
	var entry model.UserRouteEntry
	require.NoError(t, db.Where("group_id = ? AND channel_id = ?", *created.Profile.ActiveGroupID, channelID).First(&entry).Error)
	require.NoError(t, db.Model(&entry).Update("enabled", false).Error)
	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup:               "default",
		ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
		ExpectedProfileVersion: created.Profile.Version,
	})
	assert.Equal(t, "entry_disabled", LiveRouteQualificationReason(err))
}

func TestRecheckLiveRouteCandidateRejectsTokenGroupChangedAfterSelection(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	// Selection ran while the token belonged to default. Changing it before
	// final qualification must not fall back to the account's broader group.
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Update("group", "__changed_group__").Error)
	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default", ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
		ExpectedProfileVersion: created.Profile.Version,
	})
	assert.Equal(t, ShadowFilterGroupForbidden, LiveRouteQualificationReason(err))
}

func TestRecheckLiveRouteCandidateRejectsDisabledUserAfterSelection(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", userID).Update("status", common.UserStatusDisabled).Error)

	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default", ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
	})
	assert.Equal(t, ShadowFilterTokenForbidden, LiveRouteQualificationReason(err))
}

func TestRecheckLiveRouteCandidateRejectsCurrentPathCapability(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	created, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/messages",
		UserGroup:               "default",
		ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
		ExpectedProfileVersion: created.Profile.Version,
	})
	assert.Equal(t, ShadowFilterPathUnsupported, LiveRouteQualificationReason(err))
}

func TestRecheckLiveRouteCandidateUsesCurrentTokenAndEntitlement(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"model_limits_enabled": true,
		"model_limits":         "another-model",
	}).Error)
	qualification := LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default", ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
	}
	err = RecheckLiveRouteCandidate(qualification)
	assert.Equal(t, ShadowFilterTokenForbidden, LiveRouteQualificationReason(err))

	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(map[string]any{
		"model_limits_enabled": false,
		"model_limits":         "",
	}).Error)
	result := db.Model(&model.UserChannelEntitlement{}).
		Where("user_id = ? AND channel_id = ? AND source = ?", userID, channelID, model.RouteSourcePlatform).
		Updates(map[string]any{"status": model.RouteEntitlementStatusRevoked, "revoked_at": common.GetTimestamp()})
	require.NoError(t, result.Error)
	if result.RowsAffected == 0 {
		require.NoError(t, db.Create(&model.UserChannelEntitlement{
			UserID: userID, ChannelID: channelID, Source: model.RouteSourcePlatform,
			Status: model.RouteEntitlementStatusRevoked, RevokedAt: common.GetTimestamp(),
		}).Error)
	}
	err = RecheckLiveRouteCandidate(qualification)
	assert.Equal(t, ShadowFilterEntitlementRevoked, LiveRouteQualificationReason(err))
}

func TestRecheckLiveRouteCandidateRejectsStaleActiveSnapshot(t *testing.T) {
	db := setupRouteProfileTest(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	_, err := CreateUserRouteProfile(RouteProfileInput{
		UserID: userID, TokenID: tokenID, Mode: model.RouteModeManual,
		Groups: []RouteGroupInput{{
			Name: "live", Enabled: true,
			Entries: []RouteEntryInput{{ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}},
		}},
	})
	require.NoError(t, err)
	groups, marshalErr := common.Marshal([]string{"default"})
	require.NoError(t, marshalErr)
	endpoints, marshalErr := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, marshalErr)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{
		ActiveVersion: 1, SourceHash: fmt.Sprintf("preview-source-%d", channelID), CatalogVersion: "preview-catalog",
	}, "preview-source-v2", "preview-catalog-v2", []model.ChannelModelCapability{{
		RequestModel: "gpt-test", ActualModel: "gpt-test", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))

	err = RecheckLiveRouteCandidate(LiveRouteCandidateQualificationRequest{
		Context: context.Background(), RouteSource: RouteSourceManual,
		UserID: userID, TokenID: tokenID, ChannelID: channelID,
		RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		UserGroup: "default", ExpectedSnapshotVersion: 1, ExpectedCatalogVersion: "preview-catalog",
	})
	assert.Equal(t, ShadowFilterSnapshotStale, LiveRouteQualificationReason(err))
}

func TestLiveRouteSelectionMaxRatioOnlyRestrictsManualRoutes(t *testing.T) {
	manual := LiveRouteSelection{Source: RouteSourceManual, MaxRatio: 1.5}
	assert.True(t, manual.AllowsPriceRatio(1.5))
	assert.False(t, manual.AllowsPriceRatio(1.500001))
	assert.False(t, manual.AllowsPriceRatio(-1))

	auto := LiveRouteSelection{Source: RouteSourceAutoLab, MaxRatio: 1}
	assert.True(t, auto.AllowsPriceRatio(3))
	legacy := LiveRouteSelection{Source: RouteSourceLegacy, MaxRatio: 1}
	assert.True(t, legacy.AllowsPriceRatio(3))
}

func TestManualRouteAttemptsHonorPolicyWithoutExpandingSystemLimit(t *testing.T) {
	result := RouteSelectionResult{Candidates: []RouteSelectionCandidate{
		{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3},
	}, Decision: RouteDecision{Candidates: []RouteDecisionCandidate{
		{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3},
	}}}
	channelIDs := func(attempts []RouteDecisionCandidate) []int {
		ids := make([]int, 0, len(attempts))
		for _, attempt := range attempts {
			ids = append(ids, attempt.ChannelID)
		}
		return ids
	}
	assert.Equal(t, []int{1}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{Mode: model.RoutePolicyRetryNone})))
	assert.Equal(t, []int{1, 1}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{
		Mode: model.RoutePolicyRetrySameChannel, MaxSameResourceAttempts: 9,
	})))
	assert.Equal(t, []int{1, 2}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{
		Mode: model.RoutePolicyRetryNextChannel, MaxFailoverAttempts: 1,
	})))
	assert.Equal(t, []int{1, 1, 2, 2, 3, 3}, channelIDs(manualRouteAttemptCandidates(result, RouteLiveRetryPolicy{
		Mode: model.RoutePolicyRetrySameThenNext, MaxSameResourceAttempts: 1, MaxFailoverAttempts: 2,
	})))
}

func TestCandidateForAttemptSkipsCandidatesInvalidatedDuringFinalRecheck(t *testing.T) {
	selection := LiveRouteSelection{Attempts: []RouteDecisionCandidate{
		{ChannelID: 1, FilterReason: ShadowFilterChannelDisabled},
		{ChannelID: 1, FilterReason: ShadowFilterChannelDisabled},
		{ChannelID: 2},
	}}
	candidate, ok := selection.CandidateForAttempt(0)
	require.True(t, ok)
	assert.Equal(t, 2, candidate.ChannelID)
	assert.False(t, LiveRouteQualificationAllowsFailover(&LiveRouteQualificationError{Reason: ShadowFilterTokenForbidden}))
	assert.True(t, LiveRouteQualificationAllowsFailover(&LiveRouteQualificationError{Reason: ShadowFilterSnapshotStale}))
}

func TestAutoRouteAttemptsSupportBoundedSameChannelAndFailoverChoices(t *testing.T) {
	result := RouteSelectionResult{Candidates: []RouteSelectionCandidate{
		{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3},
	}, Decision: RouteDecision{Candidates: []RouteDecisionCandidate{
		{ChannelID: 1}, {ChannelID: 2}, {ChannelID: 3},
	}}}
	selection := LiveRouteSelection{Attempts: selectedRouteAttemptCandidates(result)}
	require.Equal(t, []RouteDecisionCandidate{
		{ChannelID: 1}, {ChannelID: 1}, {ChannelID: 2}, {ChannelID: 2}, {ChannelID: 3}, {ChannelID: 3},
	}, selection.Attempts)

	transient := ClassifyRouteError(503, "", "", false)
	candidate, index, found := selection.NextCandidateForError(0, 1, transient, RouteRetryCounters{TotalAttempts: 1})
	require.True(t, found)
	assert.Equal(t, 1, index)
	assert.Equal(t, 1, candidate.ChannelID)

	candidate, index, found = selection.NextCandidateForError(1, 1, transient, RouteRetryCounters{TotalAttempts: 2, SameChannelAttempts: 1})
	require.True(t, found)
	assert.Equal(t, 2, index)
	assert.Equal(t, 2, candidate.ChannelID)

	modelError := ClassifyRouteError(404, "model_not_found", "", false)
	candidate, index, found = selection.NextCandidateForError(0, 1, modelError, RouteRetryCounters{TotalAttempts: 1})
	require.True(t, found)
	assert.Equal(t, 2, index)
	assert.Equal(t, 2, candidate.ChannelID)

	_, _, found = selection.NextCandidateForError(2, 2, transient, RouteRetryCounters{TotalAttempts: 3})
	assert.False(t, found)
}
