package common

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/pkg/lifecycle"
)

// BackgroundWorkers owns periodic jobs that must finish before databases close.
var BackgroundWorkers lifecycle.Workers

// DeferredWorkers owns finite side effects accepted by requests and periodic
// jobs. Stop it after those producers, before cache and accounting flushes.
var DeferredWorkers lifecycle.Deferred

func GoDeferred(run func()) bool {
	if DeferredWorkers.Go(run) {
		return true
	}
	SysError("deferred work rejected after producers stopped")
	return false
}

func WaitBackground(ctx context.Context, duration time.Duration) bool {
	if ctx.Err() != nil {
		return false
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
