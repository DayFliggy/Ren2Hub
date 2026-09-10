package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var ErrDeferredAdmissionClosed = errors.New("deferred work submitted after its producers stopped")

// Deferred keeps accepted finite work alive until completion. Admission after
// Stop is a lifecycle violation and makes subsequent drain checks fail.
type Deferred struct {
	workers  Workers
	rejected atomic.Bool
	mu       sync.Mutex
	failure  error
}

func (d *Deferred) Go(run func()) bool {
	if d.workers.Go(func(context.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				d.mu.Lock()
				d.failure = errors.Join(d.failure, fmt.Errorf("deferred task panicked: %v", recovered))
				d.mu.Unlock()
			}
		}()
		run()
	}) {
		return true
	}
	d.rejected.Store(true)
	return false
}

func (d *Deferred) Stop(ctx context.Context) error {
	err := d.workers.Stop(ctx)
	d.mu.Lock()
	err = errors.Join(err, d.failure)
	d.mu.Unlock()
	if d.rejected.Load() {
		return errors.Join(err, ErrDeferredAdmissionClosed)
	}
	return err
}
