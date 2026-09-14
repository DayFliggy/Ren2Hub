package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayStateOwnershipAndCredentialRotation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}, &GatewayCredential{}, &GatewayStateBinding{}))
	ctx := context.Background()
	channel := &Channel{Id: 78, Key: "first\nsecond", Status: common.ChannelStatusEnabled, ChannelInfo: ChannelInfo{IsMultiKey: true}}
	require.NoError(t, db.Create(channel).Error)
	identity, err := EnsureGatewayCredential(ctx, channel.Id, "second")
	require.NoError(t, err)
	binding := GatewayStateBinding{UserID: 1, TokenID: 2, Kind: "response", ChannelID: channel.Id, CredentialID: identity.ID, EndpointID: channel.GatewayEndpointID()}
	require.NoError(t, BindGatewayState(ctx, binding, "resp_example"))
	require.NoError(t, BindGatewayState(ctx, binding, "resp_example"))
	_, err = FindGatewayState(ctx, 1, 3, "response", "resp_example")
	require.ErrorIs(t, err, ErrGatewayStateUnknown)
	_, err = FindGatewayState(ctx, 2, 2, "response", "resp_example")
	require.ErrorIs(t, err, ErrGatewayStateUnknown)
	channel.Key = "second\nfirst"
	require.NoError(t, channel.Update())
	assert.Equal(t, binding.EndpointID, channel.GatewayEndpointID())
	key, index, err := ResolveGatewayCredential(ctx, channel, identity.ID)
	require.NoError(t, err)
	assert.Equal(t, "second", key)
	assert.Equal(t, 0, index)
	channel.Key = "replacement\nfirst"
	require.NoError(t, channel.Update())
	assert.NotEqual(t, binding.EndpointID, channel.GatewayEndpointID())
	_, _, err = ResolveGatewayCredential(ctx, channel, identity.ID)
	require.ErrorIs(t, err, ErrGatewayStateUnavailable)
	channel.Key = "second\nfirst"
	require.NoError(t, channel.Update())
	assert.NotEqual(t, binding.EndpointID, channel.GatewayEndpointID(), "restoring a removed credential must not revive old state")
	channel.Key = "single-original"
	channel.ChannelInfo.IsMultiKey = false
	require.NoError(t, db.Model(channel).Update("key", channel.Key).Error)
	oldIdentity, err := EnsureGatewayCredential(ctx, channel.Id, channel.Key)
	require.NoError(t, err)
	require.NoError(t, RefreshGatewayCredential(ctx, channel, "single-refreshed"))
	newIdentity, err := EnsureGatewayCredential(ctx, channel.Id, "single-refreshed")
	require.NoError(t, err)
	assert.Equal(t, oldIdentity.ID, newIdentity.ID)
	require.Error(t, RefreshGatewayCredential(ctx, channel, "stale-refresh"))
	require.NoError(t, db.Model(&GatewayStateBinding{}).Where("token_id = ?", 2).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error)
	_, err = FindGatewayState(ctx, 1, 2, "response", "resp_example")
	require.ErrorIs(t, err, ErrGatewayStateUnknown)
}

func TestGatewayDefaultsMigrationPreservesAdminConfiguration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous; sqlDB, _ := db.DB(); _ = sqlDB.Close() })
	require.NoError(t, db.AutoMigrate(&Option{}, &Token{}))
	require.NoError(t, migrateGatewayDefaults(db))
	config, err := GetGatewayConfig()
	require.NoError(t, err)
	assert.Equal(t, 10, config.UserConcurrency)
	config.UserConcurrency = 7
	require.NoError(t, SaveGatewayConfig(config))
	require.Error(t, SaveGatewayConfig(config))
	require.NoError(t, migrateGatewayDefaults(db))
	config, err = GetGatewayConfig()
	require.NoError(t, err)
	assert.Equal(t, 7, config.UserConcurrency)
}
