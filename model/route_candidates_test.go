package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

// E2-3：启用状态和 type 57 退役都由 ChannelSatisfiesFilters 统一判定，选路行查询不再自带一份。
func TestChannelSatisfiesFiltersOwnsRoutability(t *testing.T) {
	truncateTables(t)
	rows := []*Channel{
		{Id: 920001, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled},
		{Id: 920002, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled},
		{Id: 920003, Type: constant.ChannelTypeCodex, Status: common.ChannelStatusEnabled},
		{Id: 920004, Type: constant.ChannelTypeAnthropic, Status: common.ChannelStatusEnabled},
	}
	for _, row := range rows {
		row.Name, row.Key, row.Models = "routable", "test-key", "routable-model"
		require.NoError(t, DB.Create(row).Error)
		require.NoError(t, DB.Create(&Ability{Model: "routable-model", ChannelId: row.Id, Enabled: true}).Error)
	}
	for _, row := range rows[1:3] {
		ok, kind := ChannelSatisfiesFilters(row, "routable-model", nil)
		require.False(t, ok, row.Id)
		require.Equal(t, dto.ChannelFilterKind("channel_unavailable"), kind)
	}
	found, err := RouteChannelRows(context.Background(), "routable-model", nil)
	require.NoError(t, err)
	require.Equal(t, []int{920001, 920004}, channelIDs(found))
	found, err = RouteChannelRows(context.Background(), "routable-model", []dto.ChannelFilter{{Kind: dto.FilterRequestPath, RequestPath: "/v1/responses/compact"}})
	require.NoError(t, err)
	require.Equal(t, []int{920001}, channelIDs(found), "request filters are applied by the row query itself")
}

// 与上游 GetChannel 和内存缓存相同：声明精确模型名的渠道优先，只有精确候选全部被过滤掉时
// 才回退到去掉 @ 修饰后的路由名。
func TestRouteChannelRowsPrefersExactModelName(t *testing.T) {
	truncateTables(t)
	const exact, base = "qwen3-max@thinking:on@temperature:0.2", "qwen3-max"
	for id, name := range map[int]string{930001: exact, 930002: base, 930003: base} {
		require.NoError(t, DB.Create(&Channel{Id: id, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "exact-first", Key: "test-key", Models: name}).Error)
		require.NoError(t, DB.Create(&Ability{Model: name, ChannelId: id, Enabled: true}).Error)
	}
	found, err := RouteChannelRows(context.Background(), exact, nil)
	require.NoError(t, err)
	require.Equal(t, []int{930001}, channelIDs(found), "an exact channel hides channels that only serve the routing name")
	found, err = RouteChannelRows(context.Background(), exact, []dto.ChannelFilter{{Kind: dto.FilterTokenRoute, ChannelIDs: []int{930002, 930003}}})
	require.NoError(t, err)
	require.Equal(t, []int{930002, 930003}, channelIDs(found), "the routing name is used once filters remove every exact channel")
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", 930001).Update("status", common.ChannelStatusManuallyDisabled).Error)
	found, err = RouteChannelRows(context.Background(), exact, nil)
	require.NoError(t, err)
	require.Equal(t, []int{930002, 930003}, channelIDs(found), "a disabled exact channel does not block the fallback")
}

func channelIDs(rows []*Channel) []int {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Id)
	}
	return ids
}
