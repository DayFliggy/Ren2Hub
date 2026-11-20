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
			seedSelectedRoute(t, "auto", model.TokenRouteEntry{ChannelID: 22, Enabled: true})
			require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "1").Error)
			common.MemoryCacheEnabled = cache
			if cache {
				model.InitChannelCache()
			}
			// A WebSocket create is a new call and still obeys the current Key.
			_, err := AcquirePinnedRouteChannel(routeTestContext(t), 11, "route-model")
			require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
			c := routeTestContext(t)
			original, err := AcquireOriginTaskRouteChannel(c, 11, "route-model")
			require.NoError(t, err)
			require.Equal(t, 11, original.Id)
			require.Equal(t, "1", c.GetString("channel_ratio"))
			_, err = AcquireOriginTaskRouteChannel(routeTestContext(t), 11, "route-model")
			require.ErrorIs(t, err, ErrConcurrencyFull)
			require.Equal(t, 1, ChannelInFlight(11))
			require.Zero(t, ChannelInFlight(22))
			ReleaseRouteChannel(c)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
			_, err = AcquireOriginTaskRouteChannel(routeTestContext(t), 11, "route-model")
			require.ErrorIs(t, err, errOriginTaskChannelUnavailable, "a disabled origin channel is the origin task's own error, not a Key denial")
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
		func() error {
			_, err := AcquireOriginTaskRouteChannel(routeTestContext(t), 11, "route-model")
			return err
		},
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
	scope, err := loadRouteScope(context.Background(), 7)
	require.NoError(t, err)
	_, err = routeCandidates(context.Background(), scope, "gpt-4-gizmo-a", nil)
	require.ErrorIs(t, err, failure)
	_, apiErr := SelectChannelForRequest(routeTestContext(t), "gpt-4-gizmo-a", nil)
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
}

func TestSelectedOnlyRejectsUnavailableEntries(t *testing.T) {
	for _, mode := range []string{"auto", "ordered"} {
		for _, deleted := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/deleted=%t", mode, deleted), func(t *testing.T) {
				db := setupRouteTest(t)
				createRouteChannel(t, db, 11, "route-model")
				createRouteChannel(t, db, 22, "route-model")
				seedSelectedRoute(t, mode, model.TokenRouteEntry{ChannelID: 11, Enabled: deleted})
				if deleted {
					require.NoError(t, db.Delete(&model.Channel{}, 11).Error)
					require.NoError(t, model.DeleteOrphanChannelRouteEntries(t.Context()))
				}
				channel, failure := SelectChannelForRequest(routeTestContext(t), "route-model", nil)
				require.Nil(t, channel)
				require.NotNil(t, failure)
				require.Equal(t, http.StatusServiceUnavailable, failure.StatusCode)
				require.Equal(t, types.ErrorCodeModelNotFound, failure.Code)
			})
		}
	}
}

func TestUnavailableSelectionKeepsModelNotFoundResponse(t *testing.T) {
	for _, pinned := range []bool{false, true} {
		t.Run(fmt.Sprintf("pinned=%t", pinned), func(t *testing.T) {
			setupAffinityChannels(t)
			seedSelectedRoute(t, "auto", model.TokenRouteEntry{ChannelID: 22, Enabled: false})
			c, _ := enableRouteAffinity(t, "strict", 22)
			if pinned {
				GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 22, Source: dto.PinSourceToken})
			}
			ch, failure := SelectChannelForRequest(c, "route-model", nil)
			require.Nil(t, ch)
			require.NotNil(t, failure)
			require.Equal(t, http.StatusServiceUnavailable, failure.StatusCode)
			require.Equal(t, types.ErrorCodeModelNotFound, failure.Code)
		})
	}
}

func TestAutomaticSelectedRouteSkipsOpenBreaker(t *testing.T) {
	db := setupRouteTest(t)
	resetRouteObservations(t)
	for _, id := range []int{11, 22, 33} {
		createRouteChannel(t, db, id, "route-model")
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", id).Update("priority", 100-id).Error)
	}
	seedSelectedRoute(t, "auto", model.TokenRouteEntry{ChannelID: 22, Enabled: true}, model.TokenRouteEntry{ChannelID: 33, Enabled: true})
	for range routeBreakerOpenFailures {
		recordBreaker(22, "route-model", false)
	}
	c := routeTestContext(t)
	selected, failure := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, failure)
	require.Equal(t, 33, selected.Id)
}
