package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withRouteLoadRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = previousRDB, previousEnabled
		_ = client.Close()
	})
	return server
}

// A single slow response must not move a channel out of its position, so
// observations are smoothed rather than replacing the previous value.
func TestRouteLoadSmoothsLatencyRatherThanReplacingIt(t *testing.T) {
	withRouteLoadRedis(t)
	scope := RouteLoadScope{ChannelID: 51, ActualModel: "gpt-test", Protocol: "openai"}

	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), scope, 100, 20))
	first, err := LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	require.True(t, first.LatencyKnown)
	assert.Equal(t, 100.0, first.LatencyMS, "the first sample has nothing to blend with")

	// One 1100ms outlier moves the average by alpha, not all the way.
	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), scope, 1100, 20))
	second, err := LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	assert.InDelta(t, 300.0, second.LatencyMS, 0.001,
		"0.2*1100 + 0.8*100 keeps the outlier from dominating")
	assert.Less(t, second.LatencyMS, 1100.0)
}

// An unknown metric must stay distinguishable from a measured zero, otherwise
// scoring would treat a channel with no data as instantaneous.
func TestRouteLoadReportsUnknownWhenNoSampleExists(t *testing.T) {
	withRouteLoadRedis(t)
	scope := RouteLoadScope{ChannelID: 52, ActualModel: "gpt-test", Protocol: "openai"}

	sample, err := LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	assert.False(t, sample.LatencyKnown)
	assert.False(t, sample.TTFTKnown)
	assert.False(t, sample.ErrorRateKnown)

	// Providers omit timings instead of sending zero, so a zero observation
	// must not register as a measurement.
	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), scope, 0, 0))
	sample, err = LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	assert.False(t, sample.LatencyKnown, "a zero latency is not a measurement")
	assert.False(t, sample.TTFTKnown)
	assert.True(t, sample.ErrorRateKnown, "a completed call always informs the error rate")
	assert.Equal(t, 0.0, sample.ErrorRate)
}

// A stale sample describes traffic that may no longer resemble the current
// request, so expiry must return it to unknown rather than keeping it forever.
func TestRouteLoadSampleExpires(t *testing.T) {
	server := withRouteLoadRedis(t)
	scope := RouteLoadScope{ChannelID: 53, ActualModel: "gpt-test", Protocol: "openai"}

	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), scope, 250, 30))
	sample, err := LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	require.True(t, sample.LatencyKnown)

	server.FastForward(RouteLoadSampleTTL + 1)
	sample, err = LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	assert.False(t, sample.LatencyKnown, "a sample older than the TTL is unknown, not stale data")
}

// Streaming and non-streaming calls to the same model have different latency
// profiles, and two protocols convert the same model differently. Mixing them
// would smooth away the distinction scoring depends on.
func TestRouteLoadIsolatesScopesThatAreNotComparable(t *testing.T) {
	withRouteLoadRedis(t)
	unary := RouteLoadScope{ChannelID: 54, ActualModel: "gpt-test", Protocol: "openai"}
	streaming := RouteLoadScope{ChannelID: 54, ActualModel: "gpt-test", Protocol: "openai", Stream: true}
	otherProtocol := RouteLoadScope{ChannelID: 54, ActualModel: "gpt-test", Protocol: "anthropic"}
	otherModel := RouteLoadScope{ChannelID: 54, ActualModel: "gpt-other", Protocol: "openai"}

	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), unary, 100, 10))

	for _, scope := range []RouteLoadScope{streaming, otherProtocol, otherModel} {
		sample, err := LoadRouteLoadSample(context.Background(), scope)
		require.NoError(t, err)
		assert.False(t, sample.LatencyKnown, "an unrelated scope must not inherit another scope's sample")
	}
}

// A failure's timing describes the failure, not the channel's throughput, so it
// raises the error rate without contributing a latency sample.
func TestRouteLoadErrorRaisesErrorRateWithoutLatency(t *testing.T) {
	withRouteLoadRedis(t)
	scope := RouteLoadScope{ChannelID: 55, ActualModel: "gpt-test", Protocol: "openai"}

	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), scope, 200, 20))
	require.NoError(t, ObserveRouteLoadError(context.Background(), scope))
	sample, err := LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)

	assert.Equal(t, 200.0, sample.LatencyMS, "a failure contributes no latency sample")
	assert.InDelta(t, 0.2, sample.ErrorRate, 0.001, "0.2*1 + 0.8*0")
	assert.True(t, sample.ErrorRateKnown)
}

// Load samples only order candidates; admission is decided by the Redis lease.
// Losing them degrades ranking, so a missing Redis must not fail a request.
func TestRouteLoadWithoutRedisIsNeutralNotAnError(t *testing.T) {
	previousRDB, previousEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = nil, false
	t.Cleanup(func() { common.RDB, common.RedisEnabled = previousRDB, previousEnabled })
	scope := RouteLoadScope{ChannelID: 56, ActualModel: "gpt-test", Protocol: "openai"}

	require.NoError(t, ObserveRouteLoadSuccess(context.Background(), scope, 100, 10))
	require.NoError(t, ObserveRouteLoadError(context.Background(), scope))
	sample, err := LoadRouteLoadSample(context.Background(), scope)
	require.NoError(t, err)
	assert.False(t, sample.LatencyKnown)
	assert.False(t, sample.ErrorRateKnown)
}
