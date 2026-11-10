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
	require.NoError(t, db.Create(&Channel{Id: 11, Key: "route-test-11", Status: common.ChannelStatusEnabled, Models: "route-model"}).Error)
	require.NoError(t, db.Create(&Channel{Id: 22, Key: "route-test-22", Status: common.ChannelStatusManuallyDisabled, Models: "route-model", ChannelRatio: "9"}).Error)
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

// B5：保存在事务里锁定读最终输入的全部渠道。事务外算出的 allowed 已过期，或已有条目的渠道
// 在读到旧条目之后被删除，都返回 403 且不写回孤儿条目；只确认渠道行仍在，不看启用和倍率。
func TestSaveTokenRoutingRejectsDeletedChannels(t *testing.T) {
	db := tokenRouteTestDB(t)
	ctx := context.Background()
	token := Token{UserId: 17, Key: "routing-deleted-channel"}
	require.NoError(t, token.InsertWithRouting(TokenRouteManual))
	for _, channel := range []Channel{
		{Id: 928001, Key: "deleted-1", Status: common.ChannelStatusEnabled, Models: "route-model"},
		{Id: 928002, Key: "deleted-2", Status: common.ChannelStatusAutoDisabled, Models: "route-model", ChannelRatio: "9"},
		{Id: 928003, Key: "deleted-3", Status: common.ChannelStatusEnabled, Models: "route-model"},
	} {
		require.NoError(t, db.Create(&channel).Error)
	}
	allowedFromEarlierRead := map[int]bool{928001: true, 928002: true, 928003: true}
	view, err := SaveTokenRouting(ctx, 17, token.Id, TokenRouting{
		TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: 1},
		Entries:           []TokenRouteEntry{{ChannelID: 928002, Enabled: true}, {ChannelID: 928003, Enabled: true}},
	}, allowedFromEarlierRead)
	require.NoError(t, err)

	// 新增条目：渠道在 allowed 计算之后删除并清理（Codex 探针）。
	require.NoError(t, db.Delete(&Channel{}, 928001).Error)
	require.NoError(t, DeleteOrphanChannelRouteEntries(ctx))
	added := TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: view.Version}, Entries: append(view.Entries, TokenRouteEntry{ChannelID: 928001, Enabled: true})}
	_, err = SaveTokenRouting(ctx, 17, token.Id, added, allowedFromEarlierRead)
	require.ErrorIs(t, err, ErrTokenRouteForbidden)

	// 已有条目：渠道已删除但孤儿清理还没执行或失败，旧条目不能凭"已有"被插回。
	require.NoError(t, db.Delete(&Channel{}, 928003).Error)
	kept := TokenRouting{TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: view.Version}, Entries: view.Entries}
	_, err = SaveTokenRouting(ctx, 17, token.Id, kept, nil)
	require.ErrorIs(t, err, ErrTokenRouteForbidden)

	// 用户从输入里删掉已删除的渠道后可以保存；禁用、超倍率的已有条目仍可编辑。
	view, err = SaveTokenRouting(ctx, 17, token.Id, TokenRouting{
		TokenRouteProfile: TokenRouteProfile{Mode: TokenRouteManual, Version: view.Version},
		Entries:           []TokenRouteEntry{{ChannelID: 928002, Enabled: false}},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, []TokenRouteEntry{{TokenID: token.Id, ChannelID: 928002, Position: 0, Enabled: false}}, view.Entries)
	var stored []TokenRouteEntry
	require.NoError(t, db.Where("token_id = ?", token.Id).Find(&stored).Error)
	require.Equal(t, view.Entries, stored)
}

func TestRouteChannelRowsQualifiesAllBeforeSelection(t *testing.T) {
	db := tokenRouteTestDB(t)
	for _, channel := range []Channel{{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "route-model"}, {Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "route-model"}, {Id: 3, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusAutoDisabled, Models: "route-model"}} {
		require.NoError(t, db.Create(&channel).Error)
		require.NoError(t, db.Create(&Ability{ChannelId: channel.Id, Model: "route-model", Enabled: true}).Error)
	}
	rows, err := RouteChannelRows(context.Background(), "route-model", []dto.ChannelFilter{{Kind: dto.FilterTokenRoute, ChannelIDs: []int{2, 3}}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 2, rows[0].Id)
	rows, err = RouteChannelRows(context.Background(), "route-model", []dto.ChannelFilter{{Kind: dto.FilterTokenRoute}})
	require.NoError(t, err)
	require.Empty(t, rows)
}

// B14：硬删除用户时清理该用户全部令牌（含已软删除的）的 Key 路由，其他用户的路由不受影响。
func TestHardDeleteUserRemovesTokenRouting(t *testing.T) {
	truncateTables(t)
	owner := User{Username: "route-owner", Password: "password", AffCode: "route-owner"}
	other := User{Username: "route-other", Password: "password", AffCode: "route-other"}
	require.NoError(t, DB.Create(&owner).Error)
	require.NoError(t, DB.Create(&other).Error)
	tokens := []*Token{
		{UserId: owner.Id, Key: "route-owner-active"},
		{UserId: owner.Id, Key: "route-owner-soft-deleted"},
		{UserId: other.Id, Key: "route-other-active"},
	}
	for _, token := range tokens {
		require.NoError(t, token.InsertWithRouting(TokenRouteManual))
		require.NoError(t, DB.Create(&TokenRouteEntry{TokenID: token.Id, ChannelID: 1, Enabled: true}).Error)
	}
	require.NoError(t, DB.Delete(tokens[1]).Error)

	_, err := HardDeleteUserById(owner.Id)
	require.NoError(t, err)

	var profileTokens, entryTokens []int
	require.NoError(t, DB.Model(&TokenRouteProfile{}).Pluck("token_id", &profileTokens).Error)
	require.NoError(t, DB.Model(&TokenRouteEntry{}).Pluck("token_id", &entryTokens).Error)
	require.Equal(t, []int{tokens[2].Id}, profileTokens)
	require.Equal(t, []int{tokens[2].Id}, entryTokens)
}
