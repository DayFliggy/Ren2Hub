package service

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRouteObservationsColdExpiryAndPartition(t *testing.T) {
	previous, last := routeObservations.rows, routeObservations.lastPrune
	routeObservations.rows = map[routeObservationKey]RouteObservationSnapshot{}
	t.Cleanup(func() { routeObservations.rows = previous; routeObservations.lastPrune = last })
	for range routeMinimumSamples - 1 {
		RecordRouteObservation(11, "model", 1, true, true, 500)
	}
	row := routeObservations.get(11, "model", 1, true)
	require.False(t, row.Known)
	require.False(t, row.LatencyKnown)
	RecordRouteObservation(11, "model", 1, true, false, 0)
	row = routeObservations.get(11, "model", 1, true)
	require.True(t, row.Known)
	require.False(t, row.LatencyKnown)
	require.Zero(t, routeObservations.get(11, "model", 2, true).Samples)
	require.Zero(t, routeObservations.get(11, "model", 1, false).Samples)
	key := routeObservationKey{11, "model", 1, true}
	row.UpdatedAt = time.Now().Add(-routeObservationTTL - time.Second).UnixMilli()
	routeObservations.rows[key] = row
	require.Zero(t, routeObservations.get(11, "model", 1, true).Samples)
}
func TestChannelCapAndDisabledAbilityExcludeCandidates(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	saveRouteTest(t, 11)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("channel_ratio", "1.000000000000000001").Error)
	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "1").Error)
	c := routeTestContext(t)
	scope, err := loadRouteScope(c.Request.Context(), 7)
	require.NoError(t, err)
	rows, err := routeCandidates(c.Request.Context(), scope, "route-model", nil)
	require.NoError(t, err)
	require.Empty(t, rows)
	catalog, err := CatalogRouteChannels(c.Request.Context(), 17, 7)
	require.NoError(t, err)
	require.Len(t, catalog, 1)
	require.False(t, catalog[0].Eligible)
	require.Equal(t, "channel_ratio_exceeded", catalog[0].FilterReason)
	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", nil).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 11).Update("enabled", false).Error)
	catalog, err = CatalogRouteChannels(c.Request.Context(), 17, 7)
	require.NoError(t, err)
	require.Empty(t, catalog)
	scope, err = loadRouteScope(c.Request.Context(), 7)
	require.NoError(t, err)
	rows, err = routeCandidates(c.Request.Context(), scope, "route-model", nil)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 11).Update("enabled", true).Error)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
	catalog, err = CatalogRouteChannels(c.Request.Context(), 17, 7)
	require.NoError(t, err)
	require.Len(t, catalog, 1)
	require.False(t, catalog[0].Eligible)
	require.Equal(t, "channel_unavailable", catalog[0].FilterReason)
}

// 精确模型名的渠道被 Key 倍率上限滤掉后，要回退到只声明归一化名的渠道；精确渠道可用时，
// 分发只用精确一档。
func TestExactModelTierFallsBackAfterKeyPolicy(t *testing.T) {
	db := setupRouteTest(t)
	const exact, base = "qwen3-max@thinking:on@temperature:0.2", "qwen3-max"
	createRouteChannel(t, db, 11, exact)
	createRouteChannel(t, db, 22, base)
	saveRouteTest(t, 11, 22)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("channel_ratio", "2").Error)
	candidates := func() []int {
		t.Helper()
		c := routeTestContext(t)
		scope, err := loadRouteScope(c.Request.Context(), 7)
		require.NoError(t, err)
		rows, err := routeCandidates(c.Request.Context(), scope, exact, nil)
		require.NoError(t, err)
		ids := make([]int, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.Id)
		}
		return ids
	}

	require.Equal(t, []int{11}, candidates(), "an eligible exact channel hides the routing-name channel")

	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "1").Error)
	require.Equal(t, []int{22}, candidates(), "the routing name is used once the Key ratio limit removes every exact channel")
}
