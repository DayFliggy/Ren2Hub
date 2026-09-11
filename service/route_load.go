package service

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

// RouteLoadSmoothing is the EWMA coefficient applied to every latency and
// error observation. A low weight keeps one slow response from moving a channel
// out of its position while still tracking a sustained change.
const RouteLoadSmoothing = 0.2

// RouteLoadSampleTTL bounds how long a sample stays meaningful. An older sample
// describes traffic that may no longer resemble the current request, so it is
// reported as unknown rather than as a measured value. Redis expiry implements
// this directly: a missing key and a stale key are the same condition.
const RouteLoadSampleTTL = 5 * time.Minute

// RouteLoadScope isolates samples that are not comparable. A streaming call and
// a non-streaming call to the same model have different latency profiles, and
// the same model served over two protocols is converted differently, so mixing
// them would smooth away the distinction the score depends on.
type RouteLoadScope struct {
	ChannelID   int
	ActualModel string
	Protocol    string
	Stream      bool
}

func (s RouteLoadScope) key() string {
	mode := "unary"
	if s.Stream {
		mode = "stream"
	}
	return fmt.Sprintf("route:load:v1:%d:%s:%s:%s", s.ChannelID, s.ActualModel, s.Protocol, mode)
}

// RouteLoadSample is the smoothed view of one scope. Each Known flag is
// independent because a provider may report latency without a first-token time,
// and an unknown metric must stay distinguishable from a measured zero.
type RouteLoadSample struct {
	LatencyMS      float64
	LatencyKnown   bool
	TTFTMS         float64
	TTFTKnown      bool
	ErrorRate      float64
	ErrorRateKnown bool
}

// ObserveRouteLoadSuccess folds a completed call into the scope's averages.
// A zero latency or TTFT is treated as "not reported" rather than as a
// measurement, because providers omit these instead of sending zero.
func ObserveRouteLoadSuccess(ctx context.Context, scope RouteLoadScope, latencyMS, ttftMS int64) error {
	return observeRouteLoad(ctx, scope, latencyMS, ttftMS, 0)
}

// ObserveRouteLoadError folds a failed call into the scope's error rate without
// contributing a latency sample: a failure's timing describes the failure, not
// the channel's throughput.
func ObserveRouteLoadError(ctx context.Context, scope RouteLoadScope) error {
	return observeRouteLoad(ctx, scope, 0, 0, 1)
}

func observeRouteLoad(ctx context.Context, scope RouteLoadScope, latencyMS, ttftMS int64, errorValue float64) error {
	if scope.ChannelID <= 0 || scope.ActualModel == "" {
		return nil
	}
	// Load samples only order candidates; admission is decided by the Redis
	// lease. Losing them degrades ranking rather than correctness, so a missing
	// Redis is not an error here.
	if !common.RedisEnabled || common.RDB == nil {
		return nil
	}
	if latencyMS < 0 {
		latencyMS = 0
	}
	if ttftMS < 0 {
		ttftMS = 0
	}
	return routeLoadObserveScript.Run(ctx, common.RDB, []string{scope.key()},
		RouteLoadSmoothing, latencyMS, ttftMS, errorValue, int(RouteLoadSampleTTL.Seconds())).Err()
}

// LoadRouteLoadSample reads the smoothed values for one scope. Every field is
// unknown when no sample survives, so scoring falls back to its neutral value
// instead of assuming a perfect channel.
func LoadRouteLoadSample(ctx context.Context, scope RouteLoadScope) (RouteLoadSample, error) {
	if scope.ChannelID <= 0 || scope.ActualModel == "" {
		return RouteLoadSample{}, nil
	}
	if !common.RedisEnabled || common.RDB == nil {
		return RouteLoadSample{}, nil
	}
	values, err := common.RDB.HGetAll(ctx, scope.key()).Result()
	if err != nil {
		return RouteLoadSample{}, err
	}
	sample := RouteLoadSample{}
	if latency, ok := parseRouteLoadFloat(values["latency_ms"]); ok {
		sample.LatencyMS, sample.LatencyKnown = latency, true
	}
	if ttft, ok := parseRouteLoadFloat(values["ttft_ms"]); ok {
		sample.TTFTMS, sample.TTFTKnown = ttft, true
	}
	if errorRate, ok := parseRouteLoadFloat(values["error_rate"]); ok {
		sample.ErrorRate, sample.ErrorRateKnown = clamp01(errorRate), true
	}
	return sample, nil
}

// parseRouteLoadFloat rejects absent, malformed and non-finite values so a
// corrupt entry cannot be mistaken for a measurement.
func parseRouteLoadFloat(raw string) (float64, bool) {
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}

// The update is a single script so concurrent requests cannot interleave a read
// and a write and lose an observation. Error rate always folds in; latency and
// TTFT fold in only when the provider reported them.
var routeLoadObserveScript = redis.NewScript(`
local alpha = tonumber(ARGV[1])
local latency, ttft, err_value = tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])
local ttl = tonumber(ARGV[5])

local function blend(field, value)
  local previous = redis.call('HGET', KEYS[1], field)
  if previous then
    value = alpha * value + (1 - alpha) * tonumber(previous)
  end
  redis.call('HSET', KEYS[1], field, tostring(value))
end

if latency > 0 then blend('latency_ms', latency) end
if ttft > 0 then blend('ttft_ms', ttft) end
blend('error_rate', err_value)
redis.call('EXPIRE', KEYS[1], ttl)
return 1
`)
