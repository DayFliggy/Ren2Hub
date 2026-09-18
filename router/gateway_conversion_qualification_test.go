package router

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayManualQualificationPrecedesRetryPolicy(t *testing.T) {
	for _, tc := range []struct {
		mode        string
		maxFailover int
		wantStatus  int
		wantSends   int32
	}{
		{model.RoutePolicyRetryNone, 0, 200, 1},
		{model.RoutePolicyRetrySameChannel, 0, 200, 1},
		{model.RoutePolicyRetryNextChannel, 0, 200, 1},
		{model.RoutePolicyRetryNextChannel, 1, 200, 1},
	} {
		t.Run(tc.mode+string(rune('0'+tc.maxFailover)), func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("type", constant.ChannelTypeAnthropic).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			var token model.Token
			require.NoError(t, model.DB.Where("key = ?", key).First(&token).Error)
			profile, err := service.CreateUserRouteProfile(service.RouteProfileInput{
				UserID: token.UserId, TokenID: token.Id, Mode: model.RouteModeManual,
				Groups: []service.RouteGroupInput{{Name: "review", Enabled: true, Entries: []service.RouteEntryInput{
					{ChannelID: 1, Source: model.RouteSourcePlatform, Enabled: true, Position: 0},
					{ChannelID: 4, Source: model.RouteSourcePlatform, Enabled: true, Position: 1},
				}, Policy: service.RoutePolicyInput{MaxRatio: 1, RetryMode: tc.mode, MaxFailoverAttempts: tc.maxFailover}}},
			})
			require.NoError(t, err)
			body := `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}],"reasoning_effort":"high"}`
			preview, err := service.PreviewUserRouteProfile(context.Background(), token.UserId, profile.Profile.ID, service.RouteProfilePreviewInput{Model: "gpt-4o-mini", Path: "/v1/chat/completions", Request: json.RawMessage(body)})
			require.NoError(t, err)
			require.Equal(t, 4, preview.PreferredChannelID)
			require.Equal(t, []int{4}, preview.CandidateChannelIDs)
			result := <-startGatewayRouteRequest(t, url, key, body)
			require.NoError(t, result.err)
			require.Equal(t, tc.wantStatus, result.code, result.body)
			require.Equal(t, tc.wantSends, sends.Load())
			t.Logf("mode=%s failover=%d preview=%d actualHTTP=%d upstreamSends=%d", tc.mode, tc.maxFailover, preview.PreferredChannelID, result.code, sends.Load())
		})
	}
}

func TestGatewayQueuedConversionPolicyCannotUseOldPlan(t *testing.T) {
	var sends atomic.Int32
	url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		gatewaySDKUpstream(w, r)
	})
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("capacity_total", 1).Error)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("conversion_policy", "allow").Error)
	configureGatewayManualQueue(t, key, 1)
	holder, _, err := service.AcquireGatewayChannelPermit(context.Background(), "policy-holder", 1, false, service.NewGatewayWaitBudget(0))
	require.NoError(t, err)
	defer holder.Release()
	done := startGatewayRouteRequest(t, url, key, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}]}`)
	require.Eventually(t, func() bool {
		return service.ReadGatewayAdmissionRuntime(service.ChannelAdmissionKey(1)).Queued == 1
	}, time.Second, time.Millisecond)
	require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("conversion_policy", "strict").Error)
	holder.Release()
	result := <-done
	require.NoError(t, result.err)
	assert.NotEqual(t, http.StatusOK, result.code)
	assert.Zero(t, sends.Load())
	assertGatewayRouteIdle(t, 1)
}

func TestGatewayConversionQualificationPrecedesCandidateCap(t *testing.T) {
	for _, incompatible := range []int{63, 64} {
		t.Run(string(rune('A'+incompatible-63)), func(t *testing.T) {
			var sends atomic.Int32
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id = ?", 1).Update("type", constant.ChannelTypeAnthropic).Error)
			var original model.Channel
			require.NoError(t, model.DB.Where("id = ?", 1).First(&original).Error)
			for n := 1; n < incompatible; n++ {
				channel := model.Channel{Type: constant.ChannelTypeAnthropic, Models: "gpt-4o-mini", Name: "review-incompatible", Key: "review-fixture-key", Status: common.ChannelStatusEnabled, BaseURL: original.BaseURL, Group: "default", CapacityTotal: 10, ChannelRatio: 1}
				require.NoError(t, model.DB.Create(&channel).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
			}
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			result := <-startGatewayRouteRequest(t, url, key, `{"model":"gpt-4o-mini","messages":[{"role":"user","content":"fixture"}],"reasoning_effort":"high"}`)
			require.NoError(t, result.err)
			want := http.StatusOK
			wantSends := int32(1)

			require.Equal(t, want, result.code, result.body)
			require.Equal(t, wantSends, sends.Load())
			t.Logf("incompatibleChannels=%d validChannels=1 actualHTTP=%d upstreamSends=%d", incompatible, result.code, sends.Load())
		})
	}
}
