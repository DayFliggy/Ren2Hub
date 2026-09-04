package service

import (
	"context"
	"errors"
	"math"
	"sync"
)

var ErrRouteScoreMetricsUnavailable = errors.New("route score runtime metrics are unavailable")

// RouteScoreRuntimeMetrics is deliberately separate from Channel fields. A
// zero value means "unknown", not a measured empty rate or quota.
type RouteScoreRuntimeMetrics struct {
	RateLimitHeadroom float64
	RateLimitKnown    bool
	QuotaHeadroom     float64
	QuotaKnown        bool
}

// RouteScoreMetricProvider supplies optional runtime data for both Shadow and
// Live scoring. Ren2Hub currently has no channel-wide rate-limit or quota
// telemetry source, so the default provider is nil and the fields remain
// unknown. A provider must be explicitly installed before those factors can
// affect a route.
type RouteScoreMetricProvider interface {
	ResolveRouteScoreMetrics(context.Context, int, string) (RouteScoreRuntimeMetrics, error)
}

var routeScoreMetricProvider struct {
	sync.RWMutex
	provider RouteScoreMetricProvider
}

// SetRouteScoreMetricProvider installs a process-local adapter and returns a
// restore function for tests or controlled reconfiguration.
func SetRouteScoreMetricProvider(provider RouteScoreMetricProvider) func() {
	routeScoreMetricProvider.Lock()
	previous := routeScoreMetricProvider.provider
	routeScoreMetricProvider.provider = provider
	routeScoreMetricProvider.Unlock()
	return func() {
		routeScoreMetricProvider.Lock()
		routeScoreMetricProvider.provider = previous
		routeScoreMetricProvider.Unlock()
	}
}

func LoadRouteScoreRuntimeMetrics(ctx context.Context, channelID int, requestModel string) (RouteScoreRuntimeMetrics, error) {
	routeScoreMetricProvider.RLock()
	provider := routeScoreMetricProvider.provider
	routeScoreMetricProvider.RUnlock()
	if provider == nil {
		return RouteScoreRuntimeMetrics{}, nil
	}
	metrics, err := provider.ResolveRouteScoreMetrics(ctx, channelID, requestModel)
	if err != nil {
		return RouteScoreRuntimeMetrics{}, errors.Join(ErrRouteScoreMetricsUnavailable, err)
	}
	if (metrics.RateLimitKnown && !validRouteScoreHeadroom(metrics.RateLimitHeadroom)) ||
		(metrics.QuotaKnown && !validRouteScoreHeadroom(metrics.QuotaHeadroom)) {
		return RouteScoreRuntimeMetrics{}, ErrRouteScoreMetricsUnavailable
	}
	return metrics, nil
}

func validRouteScoreHeadroom(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}
