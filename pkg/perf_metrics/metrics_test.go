package perfmetrics

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelaySamplesRespectMetricsSetting(t *testing.T) {
	metricsConfig := config.GlobalConfig.Get("perf_metrics_setting")
	previousEnabled := perf_metrics_setting.GetSetting().Enabled
	previousWorkers := sampleWorkers
	previousRedis, previousClient := common.RedisEnabled, common.RDB
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(metricsConfig, map[string]string{"enabled": strconv.FormatBool(previousEnabled)}))
		sampleWorkers = previousWorkers
		common.RedisEnabled, common.RDB = previousRedis, previousClient
		hotBuckets.Range(func(key, _ any) bool { hotBuckets.Delete(key); return true })
	})

	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { assert.NoError(t, client.Close()) })
			common.RedisEnabled, common.RDB = true, client
			sampleWorkers = &common.WorkerGroup{}
			require.NoError(t, config.UpdateConfigFromMap(metricsConfig, map[string]string{"enabled": strconv.FormatBool(enabled)}))
			RecordRelaySample(&relaycommon.RelayInfo{
				OriginModelName: "metrics-fixture",
				UsingGroup:      "default",
				StartTime:       time.Now(),
			}, true, 1)
			sampleWorkers.BeginStop()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.NoError(t, sampleWorkers.Wait(ctx))
			keys := server.Keys()
			if !enabled {
				assert.Empty(t, keys, "disabled sampling must not write to Redis")
				return
			}
			require.Len(t, keys, 1)
			assert.Equal(t, "1", server.HGet(keys[0], "req"))
			assert.Equal(t, "1", server.HGet(keys[0], "ok"))
		})
	}
}
