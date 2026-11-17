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
	db := setupCatalogRouteTest(t)
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

// 路由审计 B2：预览先判渠道是否服务所查模型，再判可用性和倍率。与该模型无关的渠道和被禁用的
// 渠道不出现在自动预览里（与模型目录一致），启用但超倍率等不可选的渠道仍带原因显示。手动列表
// 保留指向被禁用渠道的条目和原因，但不返回它的名称和倍率。
func TestRoutePreviewListsOnlyChannelsServingTheModel(t *testing.T) {
	db := setupRouteTest(t)
	require.NoError(t, db.Model(&model.Token{}).Where("id = ?", 7).Updates(map[string]any{"model_limits_enabled": true, "model_limits": "allowed-model"}).Error)
	for id, modelName := range map[int]string{11: "allowed-model", 22: "allowed-model", 33: "allowed-model", 44: "allowed-model", 88: "secret-admin-model", 99: "another-model"} {
		createRouteChannel(t, db, id, modelName)
	}
	require.NoError(t, db.Model(&model.Channel{}).Where("id IN ?", []int{22, 88}).Update("channel_ratio", "2").Error)
	// 生产禁用路径同时把 abilities 置为 disabled（B3），不能只改 status。
	for _, id := range []int{33, 99} {
		require.True(t, model.UpdateChannelStatusDetailed(id, "", common.ChannelStatusManuallyDisabled, "admin disabled").Changed)
	}
	// 渠道启用但能力行已禁用：选路和目录都按不服务该模型处理，自动预览同样不列出（修复前后一致）。
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 44).Update("enabled", false).Error)
	maxRatio := "1"
	_, err := model.SaveTokenRouting(context.Background(), 17, 7, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteAuto, MaxChannelRatio: &maxRatio}}, nil)
	require.NoError(t, err)
	query := TokenRoutePreviewQuery{Model: "allowed-model", Path: "/v1/chat/completions"}

	reasons := func() map[int]string {
		t.Helper()
		preview, err := PreviewTokenRouting(context.Background(), 17, 7, query)
		require.NoError(t, err)
		got := map[int]string{}
		for _, entry := range preview.Entries {
			got[entry.ChannelID] = entry.Reason
		}
		return got
	}
	require.Equal(t, map[int]string{11: "eligible", 22: "channel_ratio_exceeded"}, reasons())
	catalog, err := CatalogRouteChannels(context.Background(), 17, 7)
	require.NoError(t, err)
	for _, channel := range catalog {
		require.NotEqual(t, 33, channel.ID, "the disabled channel is not in the catalog either")
	}
	c := routeTestContext(t)
	scope, err := loadRouteScope(c.Request.Context(), 7)
	require.NoError(t, err)
	rows, err := routeCandidates(c.Request.Context(), scope, "allowed-model", nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 11, rows[0].Id, "the only eligible preview entry is the only dispatch candidate")

	// 手动列表保持列表顺序；无关渠道说明为不服务该模型，不报它的倍率或可用性。
	view, err := model.ReadTokenRouting(context.Background(), 7)
	require.NoError(t, err)
	_, err = model.SaveTokenRouting(context.Background(), 17, 7, model.TokenRouting{
		TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual, Version: view.Version, MaxChannelRatio: &maxRatio},
		Entries:           []model.TokenRouteEntry{{ChannelID: 88, Enabled: true}, {ChannelID: 33, Enabled: true}, {ChannelID: 22, Enabled: true}, {ChannelID: 11, Enabled: true}},
	}, map[int]bool{88: true, 33: true, 22: true, 11: true})
	require.NoError(t, err)
	preview, err := PreviewTokenRouting(context.Background(), 17, 7, query)
	require.NoError(t, err)
	got := make([][2]any, 0, len(preview.Entries))
	for _, entry := range preview.Entries {
		got = append(got, [2]any{entry.ChannelID, entry.Reason})
	}
	require.Equal(t, [][2]any{{88, "model_unavailable"}, {33, routeReasonChannelUnavailable}, {22, "channel_ratio_exceeded"}, {11, "eligible"}}, got)
	disabled := preview.Entries[1]
	require.False(t, disabled.Eligible)
	require.Empty(t, disabled.Name)
	require.Empty(t, disabled.ChannelRatio)
	encoded, err := common.Marshal(disabled)
	require.NoError(t, err)
	require.JSONEq(t, `{"channel_id":33,"eligible":false,"reason":"channel_unavailable","channel_ratio":""}`, string(encoded))
	require.NotEmpty(t, preview.Entries[2].Name, "an enabled channel keeps its name")
	require.Equal(t, "2", preview.Entries[2].ChannelRatio)
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
