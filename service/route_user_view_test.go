package service

import (
	"context"
	"testing"
	"time"

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
	for name, view := range map[string]any{"catalog": catalog, "routing channels": channels} {
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
}

// 路由审计 B2：选路与模型目录只保留服务所查模型、渠道启用、能力启用且未超 Key 倍率上限的渠道；
// 被管理员停用的渠道不出现在模型目录和选路候选中。
func TestRouteCandidatesServeOnlyEligibleChannels(t *testing.T) {
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
	// 渠道启用但能力行已禁用：选路和目录都按不服务该模型处理。
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", 44).Update("enabled", false).Error)
	maxRatio := "1"
	_, err := model.SaveTokenRouting(context.Background(), 17, 7, model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteAuto, MaxChannelRatio: &maxRatio}}, nil)
	require.NoError(t, err)

	catalog, err := CatalogRouteChannels(context.Background(), 17, 7)
	require.NoError(t, err)
	catalogByID := map[int]RouteChannel{}
	for _, channel := range catalog {
		catalogByID[channel.ID] = channel
		require.NotEqual(t, 33, channel.ID, "the disabled channel is not in the catalog")
		require.NotEqual(t, 44, channel.ID, "a channel with disabled ability is not in the catalog")
		require.NotEqual(t, 88, channel.ID, "an out-of-limit model channel is not in the catalog")
	}
	require.True(t, catalogByID[11].Eligible)
	require.False(t, catalogByID[22].Eligible)
	require.Equal(t, "channel_ratio_exceeded", catalogByID[22].FilterReason)

	c := routeTestContext(t)
	scope, err := loadRouteScope(c.Request.Context(), 7)
	require.NoError(t, err)
	rows, err := routeCandidates(c.Request.Context(), scope, "allowed-model", nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 11, rows[0].Id, "only the eligible channel enters dispatch candidates")
}

// B10: automatic ordering reads the quality bucket matching the request's stream flag.
func TestAutomaticOrderingUsesRequestedStreamBucket(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	var rows []*model.Channel
	require.NoError(t, db.Where("id IN ?", []int{11, 22}).Order("id asc").Find(&rows).Error)

	baseline := append([]*model.Channel(nil), rows...)
	orderAutomaticRoutes(baseline, "req-stream-bucket", "route-model", relayconstant.RelayModeChatCompletions, false)
	first, second := baseline[0].Id, baseline[1].Id

	// 排序读质量评估器发布的快照：降档要有同组的参考渠道，样本只写进流式桶。
	now := time.Now()
	for _, id := range []int{first, second} {
		routeQualities.invalidate(id)
		t.Cleanup(func() { routeQualities.invalidate(id) })
	}
	for range routeMinimumSamples {
		routeQualities.add(routeQualityKey{"route-model", routeObservationKey{first, "route-model", relayconstant.RelayModeChatCompletions, true}}, true, 20000, 0, 0, now)
		routeQualities.add(routeQualityKey{"route-model", routeObservationKey{second, "route-model", relayconstant.RelayModeChatCompletions, true}}, true, 100, 0, 0, now)
	}
	routeQualities.evaluate(now)

	streamed := append([]*model.Channel(nil), rows...)
	orderAutomaticRoutes(streamed, "req-stream-bucket", "route-model", relayconstant.RelayModeChatCompletions, true)
	require.NotEqual(t, first, streamed[0].Id, "slow streaming samples demote the channel in stream routing")

	nonStream := append([]*model.Channel(nil), rows...)
	orderAutomaticRoutes(nonStream, "req-stream-bucket", "route-model", relayconstant.RelayModeChatCompletions, false)
	require.Equal(t, first, nonStream[0].Id, "non-stream routing reads the non-stream bucket")
}
