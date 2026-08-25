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

func TestExecuteWithConfiguredRouteLeaseRechecksAndReleases(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	server := miniredis.RunT(t)
	originalDB, originalRedisEnabled, originalRDB := model.DB, common.RedisEnabled, common.RDB
	model.DB = db
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
		ChannelID: 7, CanonicalModel: "gpt-5", MaxChannelConcurrency: 1, Enabled: true, Version: 1,
	}).Error)

	executed := false
	err = ExecuteWithConfiguredRouteLease(
		context.Background(), "request-coordinator", 7, 10, 20, "gpt-5", time.Minute,
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 1, CapabilityVersion: 1},
		func(context.Context) (RouteLeaseRuntimeState, error) {
			return RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 1, CapabilityVersion: 1}, nil
		},
		func(context.Context, RouteLease, model.ChannelRoutePolicy) error {
			executed = true
			return nil
		},
	)
	require.NoError(t, err)
	assert.True(t, executed)

	executed = false
	err = ExecuteWithConfiguredRouteLease(
		context.Background(), "request-stale", 7, 10, 20, "gpt-5", time.Minute,
		RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 1, CapabilityVersion: 1},
		func(context.Context) (RouteLeaseRuntimeState, error) {
			return RouteLeaseRuntimeState{ChannelEnabled: true, HealthEpoch: 2, CapabilityVersion: 1}, nil
		},
		func(context.Context, RouteLease, model.ChannelRoutePolicy) error {
			executed = true
			return nil
		},
	)
	assert.ErrorIs(t, err, ErrRouteLeaseRuntime)
	assert.False(t, executed)
}
