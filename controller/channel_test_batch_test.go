package controller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestChannelTestBatchCancellationJoinsWorkers(t *testing.T) {
	for _, concurrency := range []int{1, 3, 32} {
		t.Run(string(rune('A'+concurrency)), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			channels := make([]*model.Channel, 80)
			for i := range channels {
				channels[i] = &model.Channel{Id: i + 1}
			}
			started := make(chan struct{}, 32)
			var calls atomic.Int32
			results := runChannelTestBatch(ctx, channels, concurrency, 0, func(ctx context.Context, _ *model.Channel) testResult {
				calls.Add(1)
				started <- struct{}{}
				<-ctx.Done()
				return testResult{localErr: ctx.Err()}
			})
			for range concurrency {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("worker did not start")
				}
			}
			cancel()
			count := 0
			for range results {
				count++
			}
			require.Equal(t, concurrency, count)
			require.EqualValues(t, concurrency, calls.Load(), "cancelled batches must not start queued probes")
		})
	}
}

func TestChannelTestMissingUsageCannotBecomeEstimate(t *testing.T) {
	for _, stream := range []bool{false, true} {
		usage, err := coerceTestUsage(nil, stream, 1234)
		require.NoError(t, err)
		require.Equal(t, "missing", usage.UsageSource)
		require.Zero(t, usage.TotalTokens)
		_, err = coerceTestUsage("invalid", stream, 1234)
		require.Error(t, err)
	}
}
