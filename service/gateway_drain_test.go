package service

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayDrainWaitsForRequestOwnerAfterClosingHijackedConnection(t *testing.T) {
	tracker := newGatewayRequestTracker()
	requestCtx, finish, err := tracker.track(context.Background())
	require.NoError(t, err)
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	TrackGatewayConnection(requestCtx, server)
	drainCtx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() { done <- tracker.drain(drainCtx, time.Second) }()
	<-requestCtx.Done()
	_, err = client.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF)
	select {
	case <-done:
		t.Fatal("drain returned before the request owner completed cleanup")
	default:
	}
	finish()
	finish()
	require.ErrorIs(t, <-done, context.Canceled)
	assert.Empty(t, tracker.active)
}

func TestGatewayDrainKeepsExistingRequestAndRejectsNew(t *testing.T) {
	tracker := newGatewayRequestTracker()
	requestCtx, finish, err := tracker.track(context.Background())
	require.NoError(t, err)
	tracker.Lock()
	tracker.draining = true
	tracker.Unlock()
	_, _, err = tracker.track(context.Background())
	require.ErrorIs(t, err, ErrGatewayDraining)
	assert.NoError(t, requestCtx.Err())
	done := make(chan error, 1)
	go func() { done <- tracker.drain(context.Background(), time.Second) }()
	finish()
	require.NoError(t, <-done)
}

func TestGatewayDrainCleanupRemainsBoundedAndLateHijackCloses(t *testing.T) {
	tracker := newGatewayRequestTracker()
	requestCtx, finish, err := tracker.track(context.Background())
	require.NoError(t, err)
	defer finish()
	drainCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = tracker.drain(drainCtx, 0)
	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "cleanup deadline exceeded")
	server, client := net.Pipe()
	defer client.Close()
	TrackGatewayConnection(requestCtx, server)
	_, err = client.Read(make([]byte, 1))
	assert.ErrorIs(t, err, io.EOF)
}
