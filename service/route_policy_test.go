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

func TestBuildRouteLeaseResourcesUsesIndependentPolicyLimits(t *testing.T) {
	resources, err := BuildRouteLeaseResources(model.ChannelRoutePolicy{
		ChannelID:             7,
		CanonicalModel:        "gpt-5",
		MaxUserConcurrency:    3,
		MaxTokenConcurrency:   2,
		MaxChannelConcurrency: 8,
		Enabled:               true,
		Version:               1,
	}, 10, 20)
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
	}, 10, 20)
	assert.ErrorIs(t, err, ErrRoutePolicyInvalid)

	_, err = BuildRouteLeaseResources(model.ChannelRoutePolicy{
		ChannelID: 7, CanonicalModel: "gpt-5", Enabled: true, Version: 1,
	}, 10, 20)
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
