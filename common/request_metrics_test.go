package common

import (
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func resetRequestMetricsForTest() {
	requestMetrics.Lock()
	defer requestMetrics.Unlock()
	requestMetrics.buckets = [requestMetricsBuckets]requestMetricsBucketState{}
	requestMetrics.total, requestMetrics.success = 0, 0
}

func TestRequestOutcomeTotalsKeepProcessLifetimeCounts(t *testing.T) {
	resetRequestMetricsForTest()
	t.Cleanup(resetRequestMetricsForTest)
	ObserveRequestOutcome(true)
	ObserveRequestOutcome(false)
	ObserveRequestOutcome(true)

	// Aging out the short rolling history must not reset the trend baseline.
	requestMetrics.Lock()
	requestMetrics.buckets = [requestMetricsBuckets]requestMetricsBucketState{}
	requestMetrics.Unlock()
	total, successes := RequestOutcomeTotals()
	assert.Equal(t, uint64(3), total)
	assert.Equal(t, uint64(2), successes)
	ObserveRequestOutcome(true)
	nextTotal, nextSuccesses := RequestOutcomeTotals()
	assert.Equal(t, uint64(1), nextTotal-total)
	assert.Equal(t, uint64(1), nextSuccesses-successes)
}

func TestRequestOutcomeTotalsKeepConcurrentSuccessesTogether(t *testing.T) {
	resetRequestMetricsForTest()
	t.Cleanup(resetRequestMetricsForTest)
	const requests = 16
	start := make(chan struct{})
	snapshots := make(chan [2]uint64, requests)
	var workers sync.WaitGroup
	for range requests {
		workers.Go(func() {
			<-start
			ObserveRequestOutcome(true)
			total, successes := RequestOutcomeTotals()
			snapshots <- [2]uint64{total, successes}
		})
	}
	close(start)
	workers.Wait()
	close(snapshots)
	for snapshot := range snapshots {
		assert.Equal(t, snapshot[0], snapshot[1], "all observed requests succeeded; no window may see a partial outcome")
	}
	total, successes := RequestOutcomeTotals()
	assert.Equal(t, uint64(requests), total)
	assert.Equal(t, total, successes)
}

func TestGetRequestSuccessRateRequiresMinimumSamples(t *testing.T) {
	resetRequestMetricsForTest()
	t.Cleanup(resetRequestMetricsForTest)

	for i := 0; i < requestMetricsMinSamples-1; i++ {
		ObserveRequest(http.StatusOK)
	}
	if got := GetRequestSuccessRate(); got != nil {
		t.Fatalf("expected nil rate below minimum sample count, got %v", *got)
	}

	ObserveRequest(http.StatusInternalServerError)
	got := GetRequestSuccessRate()
	if got == nil || *got != 93.3 {
		t.Fatalf("expected 93.3%% after minimum samples, got %v", got)
	}
}

func TestGetRequestSuccessRateCountsBusinessFailures(t *testing.T) {
	resetRequestMetricsForTest()
	t.Cleanup(resetRequestMetricsForTest)

	for i := 0; i < requestMetricsMinSamples-1; i++ {
		ObserveRequestOutcome(true)
	}
	ObserveRequestOutcome(false)

	got := GetRequestSuccessRate()
	if got == nil || *got != 93.3 {
		t.Fatalf("expected HTTP 200 business failure to lower rate to 93.3%%, got %v", got)
	}
}
