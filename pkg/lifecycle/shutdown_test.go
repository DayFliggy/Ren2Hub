package lifecycle

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStopAllCollectsFailuresFromEveryWorker(t *testing.T) {
	first := errors.New("write confirmation missing")
	second := errors.New("upstream cleanup failed")
	err := StopAll(context.Background(),
		Stopper{Name: "billing", Stop: func(context.Context) error { return first }},
		Stopper{Name: "relay", Stop: func(context.Context) error { return second }},
	)
	require.ErrorIs(t, err, first)
	require.ErrorIs(t, err, second)
	require.Contains(t, err.Error(), "billing")
	require.Contains(t, err.Error(), "relay")
}

func TestStopAllBoundsUnresponsiveStopper(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- StopAll(ctx, Stopper{Name: "blocked", Stop: func(context.Context) error {
			defer close(finished)
			close(started)
			<-release
			return nil
		}})
	}()
	<-started
	cancel()
	select {
	case err := <-result:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("shutdown ignored its deadline")
	}
	close(release)
	<-finished
}

func TestDeferredDrainRejectsLateWorkWithoutRunningIt(t *testing.T) {
	var deferred Deferred
	started := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	require.True(t, deferred.Go(func() { close(started); <-release; close(finished) }))
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, deferred.Stop(ctx), context.Canceled)
	require.False(t, deferred.Go(func() { t.Error("late work ran after drain sealed admission") }))
	close(release)
	<-finished
	finishContext, finish := context.WithTimeout(context.Background(), time.Second)
	defer finish()
	require.ErrorIs(t, deferred.Stop(finishContext), ErrDeferredAdmissionClosed)
}

func TestDeferredPanicPreventsSuccessfulDrain(t *testing.T) {
	var deferred Deferred
	require.True(t, deferred.Go(func() { panic("write failed") }))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := deferred.Stop(ctx)
	require.ErrorContains(t, err, "deferred task panicked: write failed")
}
