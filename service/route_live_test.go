package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
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
