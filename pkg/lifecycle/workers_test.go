package lifecycle

import (
	"context"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestStopCancelsAndWaitsForWorkerCleanup(t *testing.T) {
	var workers Workers
	canceled := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	require.True(t, workers.Go(func(ctx context.Context) { <-ctx.Done(); close(canceled); <-release; close(exited) }))
	stopped := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	go func() { stopped <- workers.Stop(ctx) }()
	<-canceled
	select {
	case <-stopped:
		t.Fatal("stop returned before cleanup")
	default:
	}
	assert.False(t, workers.Go(func(context.Context) { t.Error("work admitted after stop") }))
	close(release)
	require.NoError(t, <-stopped)
	<-exited
	require.NoError(t, workers.Stop(ctx))
}

func TestTimedOutStopCanBeWaitedAgain(t *testing.T) {
	var workers Workers
	release := make(chan struct{})
	require.True(t, workers.Go(func(context.Context) { <-release }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, workers.Stop(ctx), context.Canceled)
	close(release)
	ctx, finish := context.WithTimeout(context.Background(), time.Second)
	defer finish()
	require.NoError(t, workers.Stop(ctx))
}
