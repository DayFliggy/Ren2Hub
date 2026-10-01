package common

import (
	"math"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

const (
	requestMetricsBucket     = 5 * time.Minute
	requestMetricsBuckets    = 12
	requestMetricsMinSamples = 15
)

type requestMetricsBucketState struct {
	start   int64
	total   int64
	success int64
}

var requestMetrics = struct {
	sync.Mutex
	buckets [requestMetricsBuckets]requestMetricsBucketState
}{}

var applicationTraffic struct {
	requestBytes  atomic.Uint64
	responseBytes atomic.Uint64
}

// Process-lifetime relay outcome counters; samplers derive windowed rates
// from their deltas.
var requestOutcomeTotals struct {
	total   atomic.Uint64
	success atomic.Uint64
}

func RequestOutcomeTotals() (total, success uint64) {
	return requestOutcomeTotals.total.Load(), requestOutcomeTotals.success.Load()
}

func ObserveApplicationRequestBytes(n int) {
	if n > 0 {
		applicationTraffic.requestBytes.Add(uint64(n))
	}
}

func ObserveApplicationResponseBytes(n int) {
	if n > 0 {
		applicationTraffic.responseBytes.Add(uint64(n))
	}
}

func ApplicationTrafficSnapshot() (requestBytes, responseBytes uint64) {
	return applicationTraffic.requestBytes.Load(), applicationTraffic.responseBytes.Load()
}

func ObserveRequest(status int) {
	ObserveRequestOutcome(status >= http.StatusOK && status < http.StatusBadRequest)
}

func ObserveRequestOutcome(success bool) {
	requestOutcomeTotals.total.Add(1)
	if success {
		requestOutcomeTotals.success.Add(1)
	}
	now := time.Now().Unix()
	start := now - now%int64(requestMetricsBucket.Seconds())
	requestMetrics.Lock()
	defer requestMetrics.Unlock()

	index := int((start / int64(requestMetricsBucket.Seconds())) % requestMetricsBuckets)
	bucket := &requestMetrics.buckets[index]
	if bucket.start != start {
		*bucket = requestMetricsBucketState{start: start}
	}
	bucket.total++
	if success {
		bucket.success++
	}
}

// GetRequestSuccessRate uses the smallest recent five-minute window with at
// least the minimum sample count, expanding up to one hour when traffic is low.
func GetRequestSuccessRate() *float64 {
	now := time.Now().Unix()
	currentStart := now - now%int64(requestMetricsBucket.Seconds())
	requestMetrics.Lock()
	defer requestMetrics.Unlock()

	var total, success int64
	for i := 0; i < requestMetricsBuckets; i++ {
		start := currentStart - int64(i)*int64(requestMetricsBucket.Seconds())
		index := int((start / int64(requestMetricsBucket.Seconds())) % requestMetricsBuckets)
		bucket := requestMetrics.buckets[index]
		if bucket.start != start {
			continue
		}
		total += bucket.total
		success += bucket.success
		if total < requestMetricsMinSamples {
			continue
		}
		rate := math.Round(float64(success)/float64(total)*1000) / 10
		return &rate
	}
	return nil
}
