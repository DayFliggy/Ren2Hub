package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteChannelLoadsUseLocalOccupancyWithoutRedis(t *testing.T) {
	ctx := context.Background()
	permit, err := gatewayAdmission.TryAcquire(ctx, ChannelAdmissionKey(1), AdmissionPolicy{Capacity: 10, Revision: admissionRevision.Load()})
	require.NoError(t, err)
	defer permit.Release()
	channels := map[int]*model.Channel{1: {Id: 1, CapacityTotal: 10, ChannelRatio: 1}}
	previous := common.RedisEnabled
	common.RedisEnabled = false
	defer func() { common.RedisEnabled = previous }()
	loads, err := LoadRouteChannelLoads(ctx, channels)
	require.NoError(t, err)
	assert.InDelta(t, .1, loads[1], .000001)
}

func TestLivePlanningReadsRecoveryWithoutClaimingProbe(t *testing.T) {
	db := setupRouteProfileTest(t)
	useRoutePlanningRedis(t)
	userID, tokenID, channelID := seedRouteProfileFixture(t, db)
	publishRoutePreviewCapability(t, channelID, []string{string(constant.EndpointTypeOpenAI)}, model.RouteCapabilityStateEligible)
	require.NoError(t, db.Create(&model.ChannelHealth{
		ChannelID: channelID, Model: "gpt-test", State: model.RouteHealthStateOpen,
		CooldownUntil: time.Now().Add(-time.Second).Unix(), HealthEpoch: 8, FailureCount: 3,
	}).Error)
	for range 2 {
		selection, err := SelectLiveTokenRoute(LiveRouteRequest{
			Context: context.Background(), CapabilityEnabled: true, RequestID: "read-only", UserID: userID, TokenID: tokenID,
			RequestModel: "gpt-test", RequestPath: "/v1/chat/completions",
		})
		require.NoError(t, err)
		assert.Equal(t, int64(8), selection.Attempts[0].HealthEpoch)
	}
	health, err := LoadRouteHealth(context.Background(), channelID, "gpt-test")
	require.NoError(t, err)
	assert.Equal(t, model.RouteHealthStateOpen, health.State)
	assert.Empty(t, health.ProbeOwner)
	assert.Equal(t, int64(8), health.HealthEpoch)
}
