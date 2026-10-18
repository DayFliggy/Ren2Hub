package service

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http"
	"testing"
)

func TestTokenRouteFirstRetryAndNextRequest(t *testing.T) {
	db := setupRouteTest(t)
	for _, id := range []int{11, 22, 33} {
		createRouteChannel(t, db, id, "route-model")
	}
	saveRouteTest(t, 33, 22, 11)
	c := routeTestContext(t)
	for _, want := range []int{33, 22, 11} {
		channel, err := selectQualifiedChannel(c, "route-model")
		require.NoError(t, err)
		require.Equal(t, want, channel.Id)
		AppendUsedChannel(c, channel.Id)
	}
	channel, err := selectQualifiedChannel(c, "route-model")
	require.NoError(t, err)
	require.Nil(t, channel)
	require.NoError(t, db.Model(&model.TokenRouteEntry{}).Where("token_id = ? AND channel_id = ?", 7, 22).Update("enabled", false).Error)
	require.ErrorIs(t, CheckLockedTokenRoute(routeTestContext(t), 22, "route-model"), model.ErrTokenRouteForbidden)
	require.NoError(t, CheckLockedTokenRoute(routeTestContext(t), 11, "route-model"))
}
func TestWebSocketContinuationRespectsCurrentManualAndRatio(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	saveRouteTest(t, 11)
	c := routeTestContext(t)
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 22, Source: dto.PinSourceToken})
	_, err := AcquirePinnedRouteChannel(c, 22, "route-model")
	require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "0.5").Error)
	_, err = AcquirePinnedRouteChannel(routeTestContext(t), 11, "route-model")
	require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
}

func TestOriginTaskContinuationKeepsOriginalChannelDespiteRouteEdits(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprint("cache=", cache), func(t *testing.T) {
			db := setupRouteTest(t)
			createRouteChannel(t, db, 11, "route-model")
			createRouteChannel(t, db, 22, "route-model")
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("max_concurrency", 1).Error)
			saveRouteTest(t, 22)
			require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "0.5").Error)
			common.MemoryCacheEnabled = cache
			if cache {
				model.InitChannelCache()
			}
			// A WebSocket create is a new call and still obeys the current Key.
			_, err := AcquirePinnedRouteChannel(routeTestContext(t), 11, "route-model")
			require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
			c := routeTestContext(t)
			original, err := AcquireOriginTaskRouteChannel(c, 11)
			require.NoError(t, err)
			require.Equal(t, 11, original.Id)
			require.Equal(t, "1", c.GetString("channel_ratio"))
			_, err = AcquireOriginTaskRouteChannel(routeTestContext(t), 11)
			require.ErrorIs(t, err, ErrConcurrencyFull)
			require.Equal(t, 1, ChannelInFlight(11))
			require.Zero(t, ChannelInFlight(22))
			ReleaseRouteChannel(c)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
			_, err = AcquireOriginTaskRouteChannel(routeTestContext(t), 11)
			require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
			require.Zero(t, ChannelInFlight(11))
		})
	}
}

func TestPinnedRouteAcquisitionErrorsKeepTechnicalFailuresPrivate(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	failure := errors.New("database password=private-route-fixture")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("routing:acquire-error", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			tx.AddError(failure)
		}
	}))
	for _, acquire := range []func() error{
		func() error { _, err := AcquirePinnedRouteChannel(routeTestContext(t), 11, "route-model"); return err },
		func() error { _, err := AcquireOriginTaskRouteChannel(routeTestContext(t), 11); return err },
	} {
		err := acquire()
		require.ErrorIs(t, err, failure)
		apiErr := PinnedRouteError(err)
		require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
		require.Equal(t, types.ErrorCodeGetChannelFailed, apiErr.GetErrorCode())
		require.True(t, types.IsSkipRetryError(apiErr))
		require.NotContains(t, apiErr.ToOpenAIError().Message, "private-route-fixture")
		require.Zero(t, ChannelInFlight(11))
	}
	for _, tc := range []struct {
		err    error
		status int
		code   types.ErrorCode
	}{
		{model.ErrTokenRouteForbidden, 403, types.ErrorCodeAccessDenied},
		{ErrConcurrencyFull, 429, "channel_concurrency_exceeded"},
		{context.Canceled, 408, "request_cancelled"},
		{context.DeadlineExceeded, 408, "request_cancelled"},
	} {
		apiErr := PinnedRouteError(tc.err)
		require.Equal(t, tc.status, apiErr.StatusCode)
		require.Equal(t, tc.code, apiErr.GetErrorCode())
		require.True(t, types.IsSkipRetryError(apiErr))
	}
}
func TestRouteCandidateDatabaseErrorIsNotEmptyAvailability(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "gpt-4-gizmo-*")
	failure := errors.New("candidate database unavailable")
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("routing:query-error", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			tx.AddError(failure)
		}
	}))
	_, err := RouteCandidates(context.Background(), 7, "gpt-4-gizmo-a", nil)
	require.ErrorIs(t, err, failure)
	_, apiErr := SelectChannelForRequest(routeTestContext(t), "gpt-4-gizmo-a", nil)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
}
