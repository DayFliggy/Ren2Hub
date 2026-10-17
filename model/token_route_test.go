package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func tokenRouteTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	previous, cache := DB, common.MemoryCacheEnabled
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Token{}, &TokenRouteProfile{}, &TokenRouteEntry{}, &Channel{}, &Ability{}))
	DB = db
	common.MemoryCacheEnabled = false
	initCol()
	t.Cleanup(func() {
		DB = previous
		common.MemoryCacheEnabled = cache
		require.NoError(t, sqlDB.Close())
		if previous != nil && cache {
			InitChannelCache()
		}
	})
	return db
}

func TestTokenRouteVersionOwnershipAndEmptyManual(t *testing.T) {
	db := tokenRouteTestDB(t)
	ctx := context.Background()
	token := Token{UserId: 17, Key: "route-test"}
	require.NoError(t, token.InsertWithRouting(TokenRouteManual))
	view, err := ReadTokenRouting(ctx, token.Id)
	require.NoError(t, err)
	require.Equal(t, TokenRouteManual, view.Mode)
	require.Empty(t, view.Entries)
	input := TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: 1}, Entries: []TokenRouteEntry{{ChannelID: 22, Enabled: true, Position: 999}, {ChannelID: 11, Enabled: false, Position: -1}}}
	_, err = SaveTokenRouting(ctx, 18, token.Id, input, map[int]bool{22: true, 11: true})
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	_, err = SaveTokenRouting(ctx, 17, token.Id, input, map[int]bool{22: true})
	require.ErrorIs(t, err, ErrTokenRouteForbidden)
	view, err = SaveTokenRouting(ctx, 17, token.Id, input, map[int]bool{22: true, 11: true})
	require.NoError(t, err)
	require.Equal(t, int64(2), view.Version)
	require.Equal(t, 0, view.Entries[0].Position)
	require.Equal(t, 1, view.Entries[1].Position)
	_, err = SaveTokenRouting(ctx, 17, token.Id, input, map[int]bool{22: true, 11: true})
	require.ErrorIs(t, err, ErrTokenRouteConflict)
	input.Version = 2
	input.Entries = []TokenRouteEntry{{ChannelID: 22, Enabled: false}}
	view, err = SaveTokenRouting(ctx, 17, token.Id, input, nil)
	require.NoError(t, err, "an existing unavailable entry stays editable")
	view, err = SaveTokenRouting(ctx, 17, token.Id, TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteAuto, Version: view.Version}}, nil)
	require.NoError(t, err)
	require.Equal(t, int64(4), view.Version)
	require.Empty(t, view.Entries)
	_, err = SaveTokenRouting(ctx, 17, token.Id, TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: 0}}, nil)
	require.ErrorIs(t, err, ErrTokenRouteConflict)
	var count int64
	require.NoError(t, db.Model(&TokenRouteEntry{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestTokenRouteSelectionCacheParityAndQualification(t *testing.T) {
	db := tokenRouteTestDB(t)
	for _, channel := range []Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "route-model", Group: "default", Priority: common.GetPointer(int64(100))},
		{Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "route-model", Group: "default", Priority: common.GetPointer(int64(0))},
		{Id: 3, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "route-model", Group: "private"},
		{Id: 4, Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled, Models: "route-model", Group: "default"},
	} {
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&Ability{ChannelId: channel.Id, Model: "route-model", Group: channel.Group, Enabled: true, Priority: channel.Priority}).Error)
	}
	for _, cache := range []bool{false, true} {
		common.MemoryCacheEnabled = cache
		if cache {
			InitChannelCache()
		}
		filters := []dto.ChannelFilter{{Kind: dto.FilterTokenRoute, ChannelIDs: []int{3, 4, 2, 1}}}
		channel, err := GetRandomSatisfiedChannel("default", "route-model", 0, filters)
		require.NoError(t, err)
		require.NotNil(t, channel)
		require.Equal(t, 2, channel.Id)
		filters[0].ChannelIDs = []int{3, 4}
		channel, err = GetRandomSatisfiedChannel("default", "route-model", 0, filters)
		require.NoError(t, err)
		require.Nil(t, channel, "private preference cannot grant another group or retired type")
		filters[0].ChannelIDs = nil
		channel, err = GetRandomSatisfiedChannel("default", "route-model", 0, filters)
		require.NoError(t, err)
		require.Nil(t, channel, "empty manual must not fall back to automatic")
		channel, err = GetRandomSatisfiedChannel("default", "route-model", 0, nil)
		require.NoError(t, err)
		require.NotNil(t, channel)
		require.Equal(t, 1, channel.Id, "automatic upstream priority is unchanged")
		filters[0].ChannelIDs = []int{2}
		filters = append(filters, dto.ChannelFilter{Kind: dto.FilterResponsesWebSocket})
		channel, err = GetRandomSatisfiedChannel("default", "route-model", 0, filters)
		require.NoError(t, err)
		require.Nil(t, channel, "private channels must satisfy protocol filters")
	}
}
