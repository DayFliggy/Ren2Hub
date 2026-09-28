package perfmetrics

import (
	"context"
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/perf_metrics_setting"
	"strconv"
	"time"
)

var flushGate = make(chan struct{}, 1)
var pendingFlush = map[bucketKey]metricFlush{}

type metricFlush struct {
	id     string
	metric model.PerfMetric
}

func flushLoop(ctx context.Context) {
	for {
		interval := time.Duration(perf_metrics_setting.GetFlushIntervalMinutes()) * time.Minute
		if interval <= 0 {
			interval = time.Minute
		}
		if !common.WaitBackgroundInterval(ctx, interval) {
			return
		}
		setting := perf_metrics_setting.GetSetting()
		if !setting.Enabled {
			continue
		}
		flushCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		err := flushBuckets(flushCtx, false)
		if err == nil && setting.RetentionDays > 0 {
			cutoff := time.Now().Add(-time.Duration(setting.RetentionDays) * 24 * time.Hour).Unix()
			err = model.DB.WithContext(flushCtx).Where("bucket_ts < ?", cutoff).Delete(&model.PerfMetric{}).Error
		}
		cancel()
		if err != nil {
			common.SysError("performance metrics flush failed: " + err.Error())
		}
	}
}

// Shutdown follows the request and background producer join, then seals and
// drains accepted samples before flushing the current bucket.
func Shutdown(ctx context.Context) error {
	sampleWorkers.BeginStop()
	if err := sampleWorkers.Wait(ctx); err != nil {
		return err
	}
	return Flush(ctx)
}
func Flush(ctx context.Context) error { return flushBuckets(ctx, true) }
func acquireFlush(ctx context.Context) error {
	select {
	case flushGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-flushGate
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Caller holds flushGate. An uncertain commit is replayed with the same receipt,
// never returned to hot counters where it could be assigned a new identity.
func resolvePendingMetrics(ctx context.Context) error {
	for key, batch := range pendingFlush {
		if err := model.FlushPerfMetric(ctx, batch.id, &batch.metric); err != nil {
			return err
		}
		delete(pendingFlush, key)
	}
	return nil
}
func flushBuckets(ctx context.Context, includeCurrent bool) error {
	if err := acquireFlush(ctx); err != nil {
		return err
	}
	defer func() { <-flushGate }()
	if err := resolvePendingMetrics(ctx); err != nil {
		return err
	}
	currentBucket := bucketStart(time.Now().Unix())
	var failures []error
	hotBuckets.Range(func(rawKey, value any) bool {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			return false
		}
		key := rawKey.(bucketKey)
		if !includeCurrent && key.bucketTs >= currentBucket {
			return true
		}
		bucket := value.(*atomicBucket)
		drained := bucket.drain()
		if drained.requestCount != 0 {
			batch := metricFlush{id: common.GetUUID(), metric: model.PerfMetric{
				ModelName: key.model, Group: key.group, BucketTs: key.bucketTs,
				RequestCount: drained.requestCount, SuccessCount: drained.successCount, TotalLatencyMs: drained.totalLatencyMs,
				TtftSumMs: drained.ttftSumMs, TtftCount: drained.ttftCount, OutputTokens: drained.outputTokens, GenerationMs: drained.generationMs,
			}}
			pendingFlush[key] = batch
			if err := model.FlushPerfMetric(ctx, batch.id, &batch.metric); err != nil {
				failures = append(failures, fmt.Errorf("performance batch %s: %w", batch.id, err))
				return true
			}
			delete(pendingFlush, key)
		}
		// Serialize removal against obtaining/updating a bucket, not database I/O.
		bucketsMu.Lock()
		if key.bucketTs < bucketStart(time.Now().Add(-24*time.Hour).Unix()) && bucket.snapshot().requestCount == 0 {
			hotBuckets.Delete(rawKey)
		}
		bucketsMu.Unlock()
		return true
	})
	return errors.Join(failures...)
}

// Reads must not count a batch in both storage and memory during a flush.
func lockMetricSnapshot() (func(), error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := acquireFlush(ctx); err != nil {
		cancel()
		return nil, err
	}
	if err := resolvePendingMetrics(ctx); err != nil {
		<-flushGate
		cancel()
		return nil, err
	}
	return func() { <-flushGate; cancel() }, nil
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
