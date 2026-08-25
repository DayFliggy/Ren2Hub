package service

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCapabilityRefreshDispatcherCoalescesPendingChannel(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	var first sync.Once
	dispatcher := newCapabilityRefreshDispatcher(
		func(_ context.Context, channelID int) error {
			assert.Equal(t, 7, channelID)
			calls.Add(1)
			first.Do(func() {
				started <- struct{}{}
				<-release
			})
			return nil
		},
		func() time.Duration { return time.Second },
		nil,
	)

	dispatcher.enqueue(7)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first refresh did not start")
	}
	for range 20 {
		dispatcher.enqueue(7)
	}
	close(release)

	require.Eventually(t, dispatcher.idle, time.Second, 10*time.Millisecond)
	assert.Equal(t, int32(2), calls.Load())
}

func TestCapabilityRefreshDispatcherIgnoresInvalidChannel(t *testing.T) {
	var calls atomic.Int32
	dispatcher := newCapabilityRefreshDispatcher(
		func(context.Context, int) error {
			calls.Add(1)
			return nil
		},
		func() time.Duration { return time.Second },
		nil,
	)

	dispatcher.enqueue(0)
	dispatcher.enqueue(-1)
	assert.True(t, dispatcher.idle())
	assert.Zero(t, calls.Load())
}
