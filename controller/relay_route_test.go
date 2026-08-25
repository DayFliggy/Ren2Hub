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
