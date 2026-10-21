package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func resetHomeMetricsCache() {
	homeMetricsCache.Lock()
	homeMetricsCache.value = model.HomeRequestMetrics{}
	homeMetricsCache.expires = time.Time{}
	homeMetricsCache.Unlock()
}

// The first caller leaving must not cancel the query other visitors share.
func TestHomeMetricsSharedQuerySurvivesFirstCallerCancel(t *testing.T) {
	previous := readHomeMetrics
	resetHomeMetricsCache()
	t.Cleanup(func() { readHomeMetrics = previous; resetHomeMetricsCache() })

	entered := make(chan context.Context, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	readHomeMetrics = func(ctx context.Context, now time.Time) (model.HomeRequestMetrics, error) {
		calls.Add(1)
		entered <- ctx
		<-release
		if err := ctx.Err(); err != nil {
			return model.HomeRequestMetrics{}, err
		}
		return model.HomeRequestMetrics{Available: common.LogConsumeEnabled, Source: "test", GeneratedAt: now.Unix()}, nil
	}

	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	go func() {
		_, err := HomeMetrics(firstCtx)
		firstErr <- err
	}()
	queryCtx := <-entered
	cancelFirst()
	require.ErrorIs(t, <-firstErr, context.Canceled)
	require.NoError(t, queryCtx.Err(), "shared query inherited the first caller's cancellation")

	type outcome struct {
		value model.HomeRequestMetrics
		err   error
	}
	second := make(chan outcome, 1)
	go func() {
		value, err := HomeMetrics(context.Background())
		second <- outcome{value, err}
	}()
	close(release)
	result := <-second
	require.NoError(t, result.err)
	require.Equal(t, "test", result.value.Source)
	require.EqualValues(t, 1, calls.Load())
}
