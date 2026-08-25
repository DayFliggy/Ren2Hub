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

func routePolicyTestNow() (now time.Time) {
	return time.Unix(1_700_000_000, 0)
}
