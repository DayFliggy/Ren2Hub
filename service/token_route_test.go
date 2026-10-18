package service

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
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
func TestTokenRouteContinuationsRespectCurrentManualAndRatio(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	saveRouteTest(t, 11)
	c := routeTestContext(t)
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 22, Source: dto.PinSourceOriginTask})
	channel, err := selectQualifiedChannel(c, "route-model")
	require.NoError(t, err)
	require.Nil(t, channel)
	require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "0.5").Error)
	_, err = AcquirePinnedRouteChannel(routeTestContext(t), 11, "route-model")
	require.ErrorIs(t, err, model.ErrTokenRouteForbidden)
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
