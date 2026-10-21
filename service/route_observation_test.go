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
func TestChannelCapAndDisabledAbilityMatchPreview(t *testing.T) {
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
	preview, err := PreviewTokenRouting(c.Request.Context(), 17, 7, TokenRoutePreviewQuery{Model: "route-model", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	require.Equal(t, "channel_ratio_exceeded", preview.Entries[0].Reason)
	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", nil).Error)
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 11).Update("enabled", false).Error)
	catalog, err := CatalogRouteChannels(c.Request.Context(), 17, 7)
	require.NoError(t, err)
	require.Empty(t, catalog)
	preview, err = PreviewTokenRouting(c.Request.Context(), 17, 7, TokenRoutePreviewQuery{Model: "route-model", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	require.Equal(t, "model_unavailable", preview.Entries[0].Reason)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
	preview, err = PreviewTokenRouting(c.Request.Context(), 17, 7, TokenRoutePreviewQuery{Model: "route-model", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	require.Equal(t, "channel_unavailable", preview.Entries[0].Reason)
}

// 精确模型名的渠道被 Key 倍率上限滤掉后，要回退到只声明归一化名的渠道；精确渠道可用时，
// 分发和预览都只用精确一档。
func TestExactModelTierFallsBackAfterKeyPolicy(t *testing.T) {
	db := setupRouteTest(t)
	const exact, base = "qwen3-max@thinking:on@temperature:0.2", "qwen3-max"
	createRouteChannel(t, db, 11, exact)
	createRouteChannel(t, db, 22, base)
	saveRouteTest(t, 11, 22)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("channel_ratio", "2").Error)
	candidates := func() ([]int, map[int]string) {
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
		preview, err := PreviewTokenRouting(c.Request.Context(), 17, 7, TokenRoutePreviewQuery{Model: exact, Path: "/v1/chat/completions"})
		require.NoError(t, err)
		reasons := map[int]string{}
		for _, entry := range preview.Entries {
			reasons[entry.ChannelID] = entry.Reason
		}
		return ids, reasons
	}

	ids, reasons := candidates()
	require.Equal(t, []int{11}, ids, "an eligible exact channel hides the routing-name channel")
	require.Equal(t, map[int]string{11: "eligible", 22: "exact_model_preferred"}, reasons)

	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "1").Error)
	ids, reasons = candidates()
	require.Equal(t, []int{22}, ids, "the routing name is used once the Key ratio limit removes every exact channel")
	require.Equal(t, map[int]string{11: "channel_ratio_exceeded", 22: "eligible"}, reasons)
}
