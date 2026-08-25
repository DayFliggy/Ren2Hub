package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBuildRouteLeaseResourcesUsesSharedScopeLimits(t *testing.T) {
	resources, err := BuildRouteLeaseResources(model.ChannelRoutePolicy{
		ChannelID:             7,
		CanonicalModel:        "gpt-5",
		MaxUserConcurrency:    3,
		MaxTokenConcurrency:   2,
		MaxChannelConcurrency: 8,
		Enabled:               true,
		Version:               1,
	}, model.RouteScopeConcurrencyLimits{MaxUserConcurrency: 3, MaxTokenConcurrency: 2}, 10, 20)
	require.NoError(t, err)
	assert.Equal(t, []RouteLeaseResource{
		{Key: UserRouteLeaseKey(10), Capacity: 3},
		{Key: TokenRouteLeaseKey(20), Capacity: 2},
		{Key: ChannelModelRouteLeaseKey(7, "gpt-5"), Capacity: 8},
	}, resources)
}

func TestBuildRouteLeaseResourcesFailsClosedForDisabledOrInvalidPolicy(t *testing.T) {
	_, err := BuildRouteLeaseResources(model.ChannelRoutePolicy{
		ChannelID: 7, CanonicalModel: "gpt-5", MaxChannelConcurrency: 8, Version: 1,
	}, model.RouteScopeConcurrencyLimits{}, 10, 20)
	assert.ErrorIs(t, err, ErrRoutePolicyInvalid)

	_, err = BuildRouteLeaseResources(model.ChannelRoutePolicy{
		ChannelID: 7, CanonicalModel: "gpt-5", Enabled: true, Version: 1,
	}, model.RouteScopeConcurrencyLimits{}, 10, 20)
	assert.ErrorIs(t, err, ErrRoutePolicyInvalid)
}

func TestAcquireConfiguredRouteLeaseRequiresPolicyAndRedis(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB, originalRedisEnabled, originalRDB := model.DB, common.RedisEnabled, common.RDB
	model.DB = db
	server := miniredis.RunT(t)
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.RDB = originalDB, originalRedisEnabled, originalRDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelRoutePolicy{}))
	policy := model.ChannelRoutePolicy{ChannelID: 7, CanonicalModel: "gpt-5", MaxChannelConcurrency: 1, Enabled: true, Version: 1}
	require.NoError(t, db.Create(&policy).Error)

	lease, _, err := AcquireConfiguredRouteLease(context.Background(), "request-1", 7, 10, 20, "Gpt-5", time.Minute)
	require.NoError(t, err)
	assert.NotEmpty(t, lease.LeaseID)
	assert.NoError(t, ReleaseRouteLease(context.Background(), common.RDB, lease))

	common.RedisEnabled = false
	_, _, err = AcquireConfiguredRouteLease(context.Background(), "request-2", 7, 10, 20, "gpt-5", time.Minute)
	assert.ErrorIs(t, err, ErrRouteLeaseUnavailable)
}

func TestAcquireConfiguredRouteLeaseUsesSmallestSharedScopeLimit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB, originalRedisEnabled, originalRDB := model.DB, common.RedisEnabled, common.RDB
	model.DB = db
	server := miniredis.RunT(t)
	common.RedisEnabled = true
	common.RDB = redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.RDB = originalDB, originalRedisEnabled, originalRDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelRoutePolicy{}))
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: 7, CanonicalModel: "gpt-5", MaxUserConcurrency: 1, MaxTokenConcurrency: 1,
		MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	}).Error)
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: 8, CanonicalModel: "gpt-5", MaxUserConcurrency: 5, MaxTokenConcurrency: 5,
		MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	}).Error)

	first, _, err := AcquireConfiguredRouteLease(context.Background(), "request-low", 7, 10, 20, "gpt-5", time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ReleaseRouteLease(context.Background(), common.RDB, first) })
	_, _, err = AcquireConfiguredRouteLease(context.Background(), "request-high", 8, 10, 20, "gpt-5", time.Minute)
	assert.ErrorIs(t, err, ErrRouteLeaseCapacity)

	require.NoError(t, ReleaseRouteLease(context.Background(), common.RDB, first))
	second, _, err := AcquireConfiguredRouteLease(context.Background(), "request-high-after-release", 8, 10, 20, "gpt-5", time.Minute)
	require.NoError(t, err)
	assert.Contains(t, second.Resources, RouteLeaseResource{Key: UserRouteLeaseKey(10), Capacity: 1})
	assert.Contains(t, second.Resources, RouteLeaseResource{Key: TokenRouteLeaseKey(20), Capacity: 1})
	require.NoError(t, ReleaseRouteLease(context.Background(), common.RDB, second))
}

func TestSaveChannelRoutePolicyUsesVersionCAS(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelRoutePolicy{}))
	created, err := SaveChannelRoutePolicy(model.ChannelRoutePolicy{
		ChannelID: 11, CanonicalModel: "gpt-5", MaxChannelConcurrency: 4, Enabled: true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), created.Version)
	updated, err := SaveChannelRoutePolicy(model.ChannelRoutePolicy{
		ID: created.ID, ChannelID: 11, CanonicalModel: "gpt-5", MaxChannelConcurrency: 6, Enabled: true, Version: 1,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), updated.Version)
	_, err = SaveChannelRoutePolicy(model.ChannelRoutePolicy{
		ID: created.ID, ChannelID: 11, CanonicalModel: "gpt-5", MaxChannelConcurrency: 7, Enabled: true, Version: 1,
	})
	assert.ErrorIs(t, err, ErrRoutePolicyConflict)
}

func TestSaveChannelRoutePolicyRequiresVersionForExistingPolicy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&model.ChannelRoutePolicy{}))
	require.NoError(t, db.Create(&model.ChannelRoutePolicy{
		ChannelID: 13, CanonicalModel: "gpt-5", MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	}).Error)

	_, err = SaveChannelRoutePolicy(model.ChannelRoutePolicy{
		ChannelID: 13, CanonicalModel: "gpt-5", MaxChannelConcurrency: 3, Enabled: true,
	})
	assert.ErrorIs(t, err, ErrRoutePolicyConflict)
}

func TestRouteLiveGateRequiresPrivateRoutingCapability(t *testing.T) {
	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "false")
	t.Setenv("ROUTE_LIVE_ENABLED", "true")
	assert.False(t, RouteLiveRoutingEnabled())

	t.Setenv("TOKEN_PRIVATE_ROUTING_ENABLED", "true")
	assert.True(t, RouteLiveRoutingEnabled())
}

func TestRouteLiveRolloutUsesDedicatedAllowlists(t *testing.T) {
	originalNodeName := common.NodeName
	common.NodeName = "route-test-instance"
	t.Cleanup(func() { common.NodeName = originalNodeName })

	input := LiveRouteRequest{UserID: 11, TokenID: 22, RequestModel: "Gpt-5"}
	assert.True(t, RouteLiveRolloutMatches(input))

	t.Setenv("ROUTE_SHADOW_MODELS", "other-model")
	assert.True(t, RouteLiveRolloutMatches(input), "Shadow rollout must not affect live rollout")
	t.Setenv("ROUTE_LIVE_USER_IDS", "12")
	assert.False(t, RouteLiveRolloutMatches(input))
	t.Setenv("ROUTE_LIVE_USER_IDS", "11")
	t.Setenv("ROUTE_LIVE_TOKEN_IDS", "23")
	assert.False(t, RouteLiveRolloutMatches(input))
	t.Setenv("ROUTE_LIVE_TOKEN_IDS", "22")
	t.Setenv("ROUTE_LIVE_MODELS", "claude-opus-5")
	assert.False(t, RouteLiveRolloutMatches(input))
	t.Setenv("ROUTE_LIVE_MODELS", "gpt-5")
	t.Setenv("ROUTE_LIVE_INSTANCES", "another-instance")
	assert.False(t, RouteLiveRolloutMatches(input))
	t.Setenv("ROUTE_LIVE_INSTANCES", "route-test-instance")
	assert.True(t, RouteLiveRolloutMatches(input))
}
