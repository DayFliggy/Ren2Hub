package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

func TestRouteAttemptOnlyCountsSentProviderOutcomesOnce(t *testing.T) {
	setupRouteTest(t)
	previous := routeObservations.rows
	routeObservations.rows = map[routeObservationKey]RouteObservationSnapshot{}
	t.Cleanup(func() { routeObservations.rows = previous })
	c := routeTestContext(t)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	info := &relaycommon.RelayInfo{OriginModelName: "model", RelayMode: 1, StartTime: time.Now()}
	FinishRouteAttempt(c, info, types.NewError(errors.New("quota"), types.ErrorCodePreConsumeTokenQuotaFailed))
	require.Empty(t, routeObservations.snapshot())
	MarkRouteAttemptSent(c, info)
	FinishRouteAttempt(c, info, types.NewErrorWithStatusCode(errors.New("bad request"), types.ErrorCodeInvalidRequest, http.StatusBadRequest))
	require.Empty(t, routeObservations.snapshot())
	MarkRouteAttemptSent(c, info)
	FinishRouteAttempt(c, info, nil)
	FinishRouteAttempt(c, info, nil)
	require.Equal(t, 1, routeObservations.snapshot()[0].Samples)
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	MarkRouteAttemptSent(c, info)
	cancel()
	FinishRouteAttempt(c, info, types.NewError(context.Canceled, types.ErrorCodeDoRequestFailed))
	require.Equal(t, 1, routeObservations.snapshot()[0].Samples)
}

func TestRouteAttemptUsesMappedModelAndOnlySuccessfulValidLatency(t *testing.T) {
	setupRouteTest(t)
	previous := routeObservations.rows
	routeObservations.rows = map[routeObservationKey]RouteObservationSnapshot{}
	t.Cleanup(func() { routeObservations.rows = previous })
	c := routeTestContext(t)
	BeginRouteObservation(c)
	common.SetContextKey(c, constant.ContextKeyChannelId, 11)
	c.Set("model_mapping", `{"alias":"actual"}`)
	info := &relaycommon.RelayInfo{OriginModelName: "alias", RelayMode: 1, IsStream: true, StartTime: time.Now()}
	MarkRouteAttemptSent(c, info)
	FinishRouteAttempt(c, info, nil)
	row := routeObservations.get(11, "actual", 1, true)
	require.Equal(t, 1, row.Samples)
	require.Zero(t, row.LatencySamples)
	require.Zero(t, routeObservations.get(11, "alias", 1, true).Samples)
	MarkRouteAttemptSent(c, info)
	info.FirstResponseTime = time.Now().Add(time.Millisecond)
	ObserveRouteStreamOutput(c, `{"choices":[{"delta":{"content":"answer"}}]}`)
	FinishRouteAttempt(c, info, nil)
	row = routeObservations.get(11, "actual", 1, true)
	require.Equal(t, 1, row.LatencySamples)
	RecordRouteObservation(11, "actual", 1, true, false, 9999)
	require.Equal(t, 1, routeObservations.get(11, "actual", 1, true).LatencySamples)
}

func TestRouteAttemptConfigurationChangeCannotRepopulateObservations(t *testing.T) {
	for _, sentBeforeChange := range []bool{false, true} {
		name := "selected_before_change"
		if sentBeforeChange {
			name = "sent_before_change"
		}
		t.Run(name, func(t *testing.T) {
			db := setupRouteTest(t)
			createRouteChannel(t, db, 11, "route-model")
			previous := routeObservations.rows
			routeObservations.rows = map[routeObservationKey]RouteObservationSnapshot{}
			t.Cleanup(func() { routeObservations.rows = previous })
			RecordRouteObservation(11, "route-model", 1, false, true, 100)
			c := routeTestContext(t)
			channel, err := selectQualifiedChannel(c, "route-model")
			require.NoError(t, err)
			common.SetContextKey(c, constant.ContextKeyChannelId, channel.Id)
			info := &relaycommon.RelayInfo{OriginModelName: "route-model", RelayMode: 1, StartTime: time.Now()}
			if sentBeforeChange {
				MarkRouteAttemptSent(c, info)
			}
			channel.Priority = common.GetPointer(int64(10))
			require.NoError(t, channel.Update())
			require.Empty(t, routeObservations.snapshot(), "committed configuration clears old samples")
			if !sentBeforeChange {
				MarkRouteAttemptSent(c, info)
			}
			FinishRouteAttempt(c, info, nil)
			require.Empty(t, routeObservations.snapshot(), "the old selection cannot publish into the new configuration")
			require.Zero(t, ChannelInFlight(channel.Id))

			_, err = selectQualifiedChannel(c, "route-model")
			require.NoError(t, err)
			MarkRouteAttemptSent(c, info)
			FinishRouteAttempt(c, info, nil)
			require.Equal(t, 1, routeObservations.get(11, "route-model", 1, false).Samples)
			model.InitChannelCache()
			require.Equal(t, 1, routeObservations.get(11, "route-model", 1, false).Samples, "periodic cache refresh keeps observations")
		})
	}
}
