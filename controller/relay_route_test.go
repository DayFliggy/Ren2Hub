package controller

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLiveRoutePriceRatioAllowedOnlyUsesManualPolicy(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 2))

	ctx.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceManual,
		MaxRatio: 1.5,
	})
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 1.5))
	assert.False(t, liveRoutePriceRatioAllowed(ctx, 2))

	ctx.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceAutoLab,
		MaxRatio: 1,
	})
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 2))
}

func TestLiveRouteFailoverAttemptCountsChannelChangesOnly(t *testing.T) {
	selection := service.LiveRouteSelection{
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: 11}, {ChannelID: 11}, {ChannelID: 12}, {ChannelID: 13},
		},
	}
	assert.Equal(t, 0, liveRouteFailoverAttempt(selection, 0))
	assert.Equal(t, 0, liveRouteFailoverAttempt(selection, 1))
	assert.Equal(t, 1, liveRouteFailoverAttempt(selection, 2))
	assert.Equal(t, 2, liveRouteFailoverAttempt(selection, 3))
}

func TestAcquireRouteAttemptLeaseReleasesCapacityAfterQualificationFailure(t *testing.T) {
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")
	t.Setenv("ROUTE_LIVE_ENABLED", "true")
	originalDB, originalRDB, originalRedisEnabled := model.DB, common.RDB, common.RedisEnabled
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	model.DB = db
	common.RDB = client
	common.RedisEnabled = true
	t.Cleanup(func() {
		model.DB, common.RDB, common.RedisEnabled = originalDB, originalRDB, originalRedisEnabled
		_ = client.Close()
		if sqlDB, closeErr := db.DB(); closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{},
		&model.UserRouteProfile{}, &model.UserRouteGroup{}, &model.UserRouteEntry{},
		&model.UserChannelEntitlement{}, &model.ChannelModelCapability{},
		&model.ChannelCapabilitySnapshot{}, &model.ChannelHealth{}, &model.ChannelRoutePolicy{},
	))

	const userID = 9101
	const channelID = 91011
	user := model.User{Id: userID, Username: "lease-qualification-user", Password: "password", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: userID, Key: "lease-qualification-token", Name: "lease-qualification-token", Status: common.TokenStatusEnabled, Group: "default", ExpiredTime: -1, UnlimitedQuota: true}
	require.NoError(t, db.Create(&token).Error)
	channel := model.Channel{Id: channelID, Type: constant.ChannelTypeOpenAI, Key: "lease-qualification-key", Name: "lease-qualification-channel", Status: common.ChannelStatusEnabled, Models: "gpt-test", Group: "default"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, db.Create(&model.Ability{ChannelId: channelID, Group: "default", Model: "gpt-test", Enabled: true}).Error)
	profile := model.UserRouteProfile{UserID: userID, TokenID: token.Id, Mode: model.RouteModeManual, Version: 1, Status: model.RouteProfileStatusEnabled}
	profile.Normalize(time.Now())
	require.NoError(t, db.Create(&profile).Error)
	group := model.UserRouteGroup{ProfileID: profile.ID, Name: "live", Kind: model.RouteGroupKindManual, Enabled: true}
	require.NoError(t, db.Create(&group).Error)
	require.NoError(t, db.Create(&model.UserRouteEntry{GroupID: group.ID, ChannelID: channelID, Source: model.RouteSourcePlatform, Enabled: true}).Error)
	require.NoError(t, db.Model(&profile).Update("active_group_id", group.ID).Error)
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: channelID, CanonicalModel: "gpt-test", MaxUserConcurrency: 1,
		MaxTokenConcurrency: 1, MaxChannelConcurrency: 1, Enabled: true, Version: 1,
	}).Error)
	groups, err := common.Marshal([]string{"default"})
	require.NoError(t, err)
	endpoints, err := common.Marshal([]string{string(constant.EndpointTypeOpenAI)})
	require.NoError(t, err)
	require.NoError(t, model.PublishChannelCapabilitySnapshot(context.Background(), channelID, model.ChannelCapabilitySnapshotFence{}, "lease-source", "lease-catalog", []model.ChannelModelCapability{{
		RequestModel: "gpt-test", ActualModel: "gpt-test", LabSlug: "openai", Source: "canonical", Confidence: 1,
		AbilityGroups: string(groups), EndpointTypes: string(endpoints), ChannelStatus: common.ChannelStatusEnabled,
		ChannelType: constant.ChannelTypeOpenAI, ProjectionVersion: model.ChannelCapabilityProjectionV1, State: model.RouteCapabilityStateEligible,
	}}))
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", channelID).Update("enabled", false).Error)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("route_live_selection", service.LiveRouteSelection{
		Source: service.RouteSourceManual,
		Decision: service.RouteDecision{
			ConfigurationVersion: 1,
			Candidates: []service.RouteDecisionCandidate{{
				ChannelID: channelID, SnapshotVersion: 1, CatalogVersion: "lease-catalog", HealthEpoch: 1,
			}},
		},
		Attempts: []service.RouteDecisionCandidate{{
			ChannelID: channelID, SnapshotVersion: 1, CatalogVersion: "lease-catalog", HealthEpoch: 1,
		}},
	})
	info := &relaycommon.RelayInfo{RequestId: "lease-qualification-request", UserId: userID, TokenId: token.Id, UserGroup: "default", OriginModelName: "gpt-test"}

	err = acquireRouteAttemptLease(c, info, &channel, 0)
	assert.ErrorIs(t, err, service.ErrLiveRouteCandidateInvalid)
	assert.Equal(t, int64(0), client.ZCard(context.Background(), service.UserRouteLeaseKey(userID)).Val())
	assert.Equal(t, int64(0), client.ZCard(context.Background(), service.TokenRouteLeaseKey(token.Id)).Val())
	assert.Equal(t, int64(0), client.ZCard(context.Background(), service.ChannelModelRouteLeaseKey(channelID, "gpt-test")).Val())
	_, hasLease := c.Get("route_live_lease")
	assert.False(t, hasLease)
	updatedValue, ok := c.Get("route_live_selection")
	require.True(t, ok)
	updated := updatedValue.(service.LiveRouteSelection)
	assert.Equal(t, service.ShadowFilterAbilityDisabled, updated.Decision.Candidates[0].FilterReason)
	assert.Equal(t, "qualification_failed", updated.Decision.LeaseState)
}
