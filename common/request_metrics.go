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
	total   uint64
	success uint64
}{}

var applicationTraffic struct {
	requestBytes  atomic.Uint64
	responseBytes atomic.Uint64
}

// Counters are read together with the same lock used to record an outcome,
// so a sampler never splits a successful request across rate windows.
func RequestOutcomeTotals() (total, success uint64) {
	requestMetrics.Lock()
	defer requestMetrics.Unlock()
	return requestMetrics.total, requestMetrics.success
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
	now := time.Now().Unix()
	start := now - now%int64(requestMetricsBucket.Seconds())
	requestMetrics.Lock()
	defer requestMetrics.Unlock()
	requestMetrics.total++
	if success {
		requestMetrics.success++
	}

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
