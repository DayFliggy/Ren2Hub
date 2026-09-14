package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

var ErrRouteScoreMetricsUnavailable = errors.New("route score runtime metrics are unavailable")

// LoadRouteChannelLoads reads only unexpired lease members. Redis time keeps
// scoring aligned with admission even when application hosts have clock skew.
func LoadRouteChannelLoads(ctx context.Context, channels map[int]*model.Channel) (map[int]float64, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return nil, ErrRouteLeaseUnavailable
	}
	now, err := common.RDB.Time(ctx).Result()
	if err != nil {
		return nil, errors.Join(ErrRouteLeaseUnavailable, err)
	}
	pipeline := common.RDB.Pipeline()
	defer pipeline.Close()
	counts := make(map[int]*redis.IntCmd, len(channels))
	capacities := make(map[int]int, len(channels))
	for channelID, channel := range channels {
		capacity, _, err := channel.RoutingSettings()
		if err != nil {
			return nil, err
		}
		capacities[channelID] = capacity
		counts[channelID] = pipeline.ZCount(ctx, ChannelRouteLeaseKey(channelID), "("+strconv.FormatInt(now.UnixMilli(), 10), "+inf")
	}
	if len(counts) > 0 {
		if _, err := pipeline.Exec(ctx); err != nil {
			return nil, errors.Join(ErrRouteLeaseUnavailable, err)
		}
	}
	loads := make(map[int]float64, len(channels))
	for channelID, count := range counts {
		loads[channelID] = float64(count.Val()) / float64(capacities[channelID])
	}
	return loads, nil
}

// RouteScoreRuntimeMetrics is deliberately separate from Channel fields. A
// zero value means "unknown", not a measured empty rate or quota.
type RouteScoreRuntimeMetrics struct {
	RateLimitHeadroom float64
	RateLimitKnown    bool
	QuotaHeadroom     float64
	QuotaKnown        bool
}

// RouteScoreMetricProvider supplies optional telemetry to diagnostic callers.
// Request planning always reads distributed lease occupancy directly.
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
