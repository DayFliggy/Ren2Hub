package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

// C1: ordinary users see pricing and capacity limits, never other users' live load.
func TestUserRouteViewsHideRuntimeOccupancy(t *testing.T) {
	db := setupRouteTest(t)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	require.NoError(t, db.Create(&model.User{Id: 17, Username: "view-owner"}).Error)
	createRouteChannel(t, db, 11, "route-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Updates(map[string]any{"channel_ratio": "1.5", "max_concurrency": 1}).Error)
	permit, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 11, MaxConcurrency: 1})
	require.NoError(t, err)
	defer permit.Release()
	require.Equal(t, 1, ChannelInFlight(11))

	catalog, err := BuildModelCatalog(context.Background(), 17, 7)
	require.NoError(t, err)
	channels, err := TokenRouteChannels(context.Background(), 17, 7)
	require.NoError(t, err)
	preview, err := PreviewTokenRouting(context.Background(), 17, 7, TokenRoutePreviewQuery{Model: "route-model", Path: "/v1/chat/completions"})
	require.NoError(t, err)
	for name, view := range map[string]any{"catalog": catalog, "routing channels": channels, "preview": preview} {
		encoded, err := common.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "in_flight", name)
	}
	for name, view := range map[string]any{"catalog": catalog.Channels, "routing channels": channels} {
		encoded, err := common.Marshal(view)
		require.NoError(t, err)
		require.Contains(t, string(encoded), `"channel_ratio":"1.5"`, name)
		require.Contains(t, string(encoded), `"max_concurrency":1`, name)
	}
	require.Len(t, preview.Entries, 1)
	require.Equal(t, "channel_concurrency_full", preview.Entries[0].Reason)
}

// B10: the preview orders with the requested stream bucket and states that
// automatic ordering is drawn again for every request.
func TestRoutePreviewUsesRequestedStreamBucket(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	query := TokenRoutePreviewQuery{Model: "route-model", Path: "/v1/chat/completions"}
	baseline, err := PreviewTokenRouting(context.Background(), 17, 7, query)
	require.NoError(t, err)
	require.Equal(t, TokenRouteOrderingWeightedPerRequest, baseline.Ordering)
	require.False(t, baseline.Stream)
	require.Len(t, baseline.Entries, 2)
	first := baseline.Entries[0].ChannelID
	for range routeMinimumSamples {
		RecordRouteObservation(first, "route-model", relayconstant.RelayModeChatCompletions, true, true, 20000)
	}
	streamQuery := query
	streamQuery.Stream = true
	streamed, err := PreviewTokenRouting(context.Background(), 17, 7, streamQuery)
	require.NoError(t, err)
	require.True(t, streamed.Stream)
	require.NotEqual(t, first, streamed.Entries[0].ChannelID, "slow streaming samples demote the channel in stream previews")
	unchanged, err := PreviewTokenRouting(context.Background(), 17, 7, query)
	require.NoError(t, err)
	require.Equal(t, first, unchanged.Entries[0].ChannelID, "non-stream previews read the non-stream bucket")
	saveRouteTest(t, 22, 11)
	manual, err := PreviewTokenRouting(context.Background(), 17, 7, query)
	require.NoError(t, err)
	require.Equal(t, TokenRouteOrderingManual, manual.Ordering)
}
