package common

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkerGroupCancelsThenJoinsAcceptedWork(t *testing.T) {
	var group WorkerGroup
	canceled, release := make(chan struct{}), make(chan struct{})
	require.True(t, group.Go(func(ctx context.Context) {
		<-ctx.Done()
		close(canceled)
		<-release
	}))
	group.BeginStop()
	group.BeginStop()
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("worker did not receive cancellation")
	}
	assert.False(t, group.Accepting())
	assert.False(t, group.Go(func(context.Context) { t.Error("late worker ran") }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, group.Wait(ctx), context.Canceled, "cancellation is not completion")
	close(release)
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, group.Wait(ctx))
}

func TestWorkerGroupStopsBeforeFirstWorker(t *testing.T) {
	var group WorkerGroup
	group.BeginStop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, group.Wait(ctx))
	assert.False(t, group.Go(func(context.Context) { t.Error("sealed group started work") }))
}

func TestBackgroundIntervalHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, WaitBackgroundInterval(ctx, time.Hour))
}
