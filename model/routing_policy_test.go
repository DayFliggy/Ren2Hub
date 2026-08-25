package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelRoutePolicyValidationAndLookup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&ChannelRoutePolicy{}))
	require.True(t, db.Migrator().HasIndex(&ChannelRoutePolicy{}, "channel_route_policy_model"))

	policy := ChannelRoutePolicy{ChannelID: 11, CanonicalModel: "gpt-5", MaxChannelConcurrency: 4, Enabled: true}
	policy.Normalize(routePolicyTestNow())
	require.NoError(t, policy.Validate())
	require.NoError(t, db.Create(&policy).Error)
	loaded, err := FindChannelRoutePolicy(context.Background(), 11, "gpt-5")
	require.NoError(t, err)
	assert.Equal(t, int64(1), loaded.Version)
	assert.Equal(t, 4, loaded.MaxChannelConcurrency)

	_, err = FindChannelRoutePolicy(context.Background(), 12, "gpt-5")
	assert.ErrorIs(t, err, ErrChannelRoutePolicyNotFound)
	assert.Error(t, (ChannelRoutePolicy{ChannelID: 11, CanonicalModel: "gpt-5", Enabled: true}).Validate())
}

func TestEnabledRouteScopeConcurrencyLimitsUseSmallestPositiveValue(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	originalDB := DB
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		sqlDB, closeErr := db.DB()
		if closeErr == nil {
			_ = sqlDB.Close()
		}
	})
	require.NoError(t, db.AutoMigrate(&ChannelRoutePolicy{}))
	require.NoError(t, db.Create(&ChannelRoutePolicy{
		ChannelID: 1, CanonicalModel: "gpt-5", MaxUserConcurrency: 0, MaxTokenConcurrency: 0,
		MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelRoutePolicy{
		ChannelID: 2, CanonicalModel: "gpt-5", MaxUserConcurrency: 4, MaxTokenConcurrency: 3,
		MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelRoutePolicy{
		ChannelID: 3, CanonicalModel: "gpt-5", MaxUserConcurrency: 2, MaxTokenConcurrency: 0,
		MaxChannelConcurrency: 2, Enabled: true, Version: 1,
	}).Error)
	require.NoError(t, db.Create(&ChannelRoutePolicy{
		ChannelID: 4, CanonicalModel: "gpt-5", MaxUserConcurrency: 1, MaxTokenConcurrency: 1,
		MaxChannelConcurrency: 2, Enabled: false, Version: 1,
	}).Error)

	limits, err := FindEnabledRouteScopeConcurrencyLimits(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 2, limits.MaxUserConcurrency)
	assert.Equal(t, 3, limits.MaxTokenConcurrency)
}

func routePolicyTestNow() (now time.Time) {
	return time.Unix(1_700_000_000, 0)
}
