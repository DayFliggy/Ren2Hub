package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
)

var flushMu sync.Mutex
var pendingFlush = map[bucketKey]*metricFlush{}

type metricFlush struct {
	id     string
	metric *model.PerfMetric
}

func flushLoop(ctx context.Context) {
	for {
		interval := time.Duration(perf_metrics_setting.GetFlushIntervalMinutes()) * time.Minute
		if interval <= 0 {
			interval = time.Minute
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		setting := perf_metrics_setting.GetSetting()
		if !setting.Enabled {
			continue
		}
		if err := flushBuckets(ctx, false); err != nil {
			common.SysError("performance flush failed: " + err.Error())
		}
		if ctx.Err() == nil {
			cleanupExpiredMetrics(setting.RetentionDays)
		}
	}
}

// Flush includes the active bucket, even if collection was subsequently disabled.
// Call after producers have drained when using it as an exit barrier.
func Flush(ctx context.Context) error { return flushBuckets(ctx, true) }

// Caller holds flushMu. Query must not double-count a batch whose database
// commit succeeded but whose acknowledgement was lost.
func resolvePendingMetrics(ctx context.Context) error {
	for key, pending := range pendingFlush {
		if err := model.FlushPerfMetric(ctx, pending.id, pending.metric); err != nil {
			return err
		}
		delete(pendingFlush, key)
	}
	return nil
}

func flushBuckets(ctx context.Context, includeCurrent bool) error {
	flushMu.Lock()
	defer flushMu.Unlock()
	var failures []error
	currentBucket := bucketStart(time.Now().Unix())
	hotBuckets.Range(func(key, value any) bool {
		k := key.(bucketKey)
		if !includeCurrent && k.bucketTs >= currentBucket {
			return true
		}
		bucket := value.(*atomicBucket)
		// Complete a prior ambiguous write with the same receipt before draining
		// newly recorded samples from this bucket.
		if pending := pendingFlush[k]; pending != nil {
			if err := model.FlushPerfMetric(ctx, pending.id, pending.metric); err != nil {
				failures = append(failures, err)
				return true
			}
			delete(pendingFlush, k)
		}
		drained := bucket.drain()
		if drained.requestCount == 0 {
			return true
		}
		pending := &metricFlush{id: common.GetUUID(), metric: &model.PerfMetric{
			ModelName: k.model, Group: k.group, BucketTs: k.bucketTs,
			RequestCount: drained.requestCount, SuccessCount: drained.successCount,
			TotalLatencyMs: drained.totalLatencyMs, TtftSumMs: drained.ttftSumMs,
			TtftCount: drained.ttftCount, OutputTokens: drained.outputTokens, GenerationMs: drained.generationMs,
		}}
		pendingFlush[k] = pending
		if err := model.FlushPerfMetric(ctx, pending.id, pending.metric); err != nil {
			failures = append(failures, fmt.Errorf("performance bucket %s/%d: %w", k.model, k.bucketTs, err))
			return true
		}
		delete(pendingFlush, k)
		// Record holds bucketsMu while obtaining and updating a bucket. Removing
		// an expired empty bucket cannot orphan a concurrent writer's pointer.
		bucketsMu.Lock()
		if k.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) && bucket.snapshot().requestCount == 0 {
			hotBuckets.Delete(key)
		}
		bucketsMu.Unlock()
		return true
	})
	return errors.Join(failures...)
}

func cleanupExpiredMetrics(retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour).Unix()
	if err := model.DeletePerfMetricsBefore(cutoff); err != nil {
		common.SysError("failed to cleanup expired perf metrics: " + err.Error())
	}
}

func redisCounters(values map[string]string) counters {
	return counters{
		requestCount:   parseRedisInt(values["req"]),
		successCount:   parseRedisInt(values["ok"]),
		totalLatencyMs: parseRedisInt(values["lat"]),
		ttftSumMs:      parseRedisInt(values["ttft"]),
		ttftCount:      parseRedisInt(values["ttft_n"]),
		outputTokens:   parseRedisInt(values["out"]),
		generationMs:   parseRedisInt(values["gen_ms"]),
	}
}

func parseRedisInt(value string) int64 {
	if value == "" {
		return 0
	}
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}
