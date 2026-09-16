package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupChannelAdmissionTest(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB, originalEnabled, originalRDB := model.DB, common.RedisEnabled, common.RDB
	client := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	model.DB, common.RedisEnabled, common.RDB = db, true, client
	t.Cleanup(func() {
		model.DB, common.RedisEnabled, common.RDB = originalDB, originalEnabled, originalRDB
		require.NoError(t, client.Close())
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Channel{}, &model.ChannelCapabilitySnapshot{}, &model.ChannelHealth{}))
	return db
}

func TestChannelAdmissionSharesCapacityAcrossModelsAndUsers(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	require.NoError(t, db.Create(&model.Channel{Id: 7, Status: common.ChannelStatusEnabled, CapacityTotal: 1, ChannelRatio: 1.5}).Error)
	ctx := context.Background()
	first, channel, err := AcquireGatewayChannelPermit(ctx, "first", 7, false, nil)
	require.NoError(t, err)
	assert.Equal(t, 1.5, channel.ChannelRatio)
	_, _, err = AcquireGatewayChannelPermit(ctx, "second", 7, false, nil)
	assert.ErrorIs(t, err, ErrAdmissionCapacity)
	first.Release()
	second, _, err := AcquireGatewayChannelPermit(ctx, "second", 7, false, nil)
	require.NoError(t, err)
	second.Release()
	common.RedisEnabled = false
	offline, _, err := AcquireGatewayChannelPermit(ctx, "offline", 7, false, nil)
	require.NoError(t, err)
	offline.Release()
}

func TestChannelAdmissionRechecksPriceAndStatusAfterCapacityUpdate(t *testing.T) {
	db := setupChannelAdmissionTest(t)
	require.NoError(t, db.Create(&model.Channel{Id: 7, Status: common.ChannelStatusEnabled, CapacityTotal: 3, ChannelRatio: 1.5}).Error)
	ctx := context.Background()
	expected, err := GetRouteRuntimeState(ctx, 7, "gpt-5")
	require.NoError(t, err)
	for _, update := range []map[string]any{{"capacity_total": 2}, {"capacity_total": 3, "channel_ratio": 2.0}} {
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7).Updates(update).Error)
		current, err := GetRouteRuntimeState(ctx, 7, "gpt-5")
		require.NoError(t, err)
		if current.ChannelRatio == expected.ChannelRatio {
			assert.NoError(t, RecheckRouteRuntime(expected, current))
		} else {
			assert.ErrorIs(t, RecheckRouteRuntime(expected, current), ErrAdmissionRuntime)
		}
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 7).Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, _, err = AcquireGatewayChannelPermit(ctx, "disabled", 7, false, nil)
	assert.ErrorIs(t, err, ErrAdmissionRuntime)
}
