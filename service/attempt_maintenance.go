package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// AttemptMaintenance maintains health probes and Provider/request reservations.
// Local admission permits never expire and are not renewed here.
type AttemptMaintenance struct {
	Done    <-chan error
	Stop    func()
	Failure func() error
}

var attemptMaintenanceCounters struct {
	active   atomic.Int64
	checks   atomic.Uint64
	failures atomic.Uint64
}

type AttemptMaintenanceMetrics struct {
	Active   int64  `json:"active"`
	Checks   uint64 `json:"checks_total"`
	Failures uint64 `json:"failures_total"`
}

func ReadAttemptMaintenanceMetrics() AttemptMaintenanceMetrics {
	return AttemptMaintenanceMetrics{Active: attemptMaintenanceCounters.active.Load(), Checks: attemptMaintenanceCounters.checks.Load(), Failures: attemptMaintenanceCounters.failures.Load()}
}

func StartAttemptMaintenance(ctx context.Context, interval time.Duration, renew func(context.Context) error) AttemptMaintenance {
	attemptMaintenanceCounters.active.Add(1)
	child, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	var failureMu sync.RWMutex
	var failureErr error
	failure := func() error {
		failureMu.RLock()
		defer failureMu.RUnlock()
		return failureErr
	}
	go func() {
		defer attemptMaintenanceCounters.active.Add(-1)
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-ticker.C:
				attemptMaintenanceCounters.checks.Add(1)
				if err := renew(child); err != nil {
					if child.Err() != nil {
						return
					}
					attemptMaintenanceCounters.failures.Add(1)
					failureMu.Lock()
					failureErr = err
					failureMu.Unlock()
					done <- err
					return
				}
			}
		}
	}()
	return AttemptMaintenance{Done: done, Stop: cancel, Failure: failure}
}
