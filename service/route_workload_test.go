package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
)

// 固定请求、时序和故障回报，使同一份输入能对照升级前后的上游放大倍数。
func TestAutomaticRoutingWorkload(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	for _, id := range []int{11, 22, 33} {
		createRouteChannel(t, db, id, "workload")
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Update("priority", 100-id).Error)
	}
	createRouteChannel(t, db, 1, "workload")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("priority", 1000).Error)
	seedSelectedRoute(t, "auto", model.TokenRouteEntry{ChannelID: 33, Enabled: true}, model.TokenRouteEntry{ChannelID: 22, Enabled: true}, model.TokenRouteEntry{ChannelID: 11, Enabled: true})
	synctest.Test(t, func(t *testing.T) {
		successes, calls := 0, 0
		for n := range 72 {
			c := routeTestContext(t)
			c.Set(common.RequestIdKey, fmt.Sprintf("routing-workload-%03d", n))
			info := &relaycommon.RelayInfo{OriginModelName: "workload", RelayMode: 1, StartTime: time.Now()}
			for round := range 3 {
				channel, selErr := SelectChannelForRequest(c, "workload", &RetryParam{Retry: &round})
				require.Nil(t, selErr)
				require.NotEqual(t, 1, channel.Id, "channel outside the selected list")
				calls++
				AppendUsedChannel(c, channel.Id)
				common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
				MarkRouteAttemptSent(c, info)
				if channel.Id == 33 {
					FinishRouteAttempt(c, info, nil)
					successes++
					break
				}
				status := http.StatusBadGateway
				if channel.Id == 11 {
					status = http.StatusTooManyRequests
					ObserveRouteResponse(c, &http.Response{StatusCode: status})
				}
				FinishRouteAttempt(c, info, types.NewOpenAIError(errors.New("upstream"), types.ErrorCodeBadResponseStatusCode, status))
			}
			time.Sleep(time.Millisecond)
		}
		t.Logf("requests=72 successes=%d calls=%d failures=%d", successes, calls, calls-successes)
		require.Equal(t, 72, successes)
		require.LessOrEqual(t, calls, 78)
	})
}
