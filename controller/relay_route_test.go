package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestLiveRoutePriceRatioAllowedOnlyUsesManualPolicy(t *testing.T) {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 2))

	ctx.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceManual,
		MaxRatio: 1.5,
	})
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 1.5))
	assert.False(t, liveRoutePriceRatioAllowed(ctx, 2))

	ctx.Set("route_live_selection", service.LiveRouteSelection{
		Source:   service.RouteSourceAutoLab,
		MaxRatio: 1,
	})
	assert.True(t, liveRoutePriceRatioAllowed(ctx, 2))
}

func TestLiveRouteFailoverAttemptCountsChannelChangesOnly(t *testing.T) {
	selection := service.LiveRouteSelection{
		Attempts: []service.RouteDecisionCandidate{
			{ChannelID: 11}, {ChannelID: 11}, {ChannelID: 12}, {ChannelID: 13},
		},
	}
	assert.Equal(t, 0, liveRouteFailoverAttempt(selection, 0))
	assert.Equal(t, 0, liveRouteFailoverAttempt(selection, 1))
	assert.Equal(t, 1, liveRouteFailoverAttempt(selection, 2))
	assert.Equal(t, 2, liveRouteFailoverAttempt(selection, 3))
}
