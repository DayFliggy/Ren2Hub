package common

import (
	"context"
	"sync"
	"time"
)

// WorkerGroup owns background work until cancellation and a bounded join.
// Its zero value accepts work; BeginStop permanently closes admission.
type WorkerGroup struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	stopping bool
}

var BackgroundWorkers = &WorkerGroup{}

func (group *WorkerGroup) initLocked() {
	if group.ctx == nil {
		group.ctx, group.cancel = context.WithCancel(context.Background())
		group.done = make(chan struct{})
	}
}

func (group *WorkerGroup) Go(run func(context.Context)) bool {
	group.mu.Lock()
	group.initLocked()
	if group.stopping {
		group.mu.Unlock()
		return false
	}
	group.wg.Add(1)
	ctx := group.ctx
	group.mu.Unlock()
	go func() {
		defer group.wg.Done()
		run(ctx)
	}()
	return true
}

func (group *WorkerGroup) Accepting() bool {
	group.mu.Lock()
	defer group.mu.Unlock()
	return !group.stopping
}

func (group *WorkerGroup) BeginStop() {
	group.mu.Lock()
	defer group.mu.Unlock()
	group.initLocked()
	if group.stopping {
		return
	}
	group.stopping = true
	group.cancel()
	go func() {
		group.wg.Wait()
		close(group.done)
	}()
}

func (group *WorkerGroup) Wait(ctx context.Context) error {
	group.mu.Lock()
	group.initLocked()
	done := group.done
	group.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WaitBackgroundInterval makes periodic loops responsive to shutdown.
func WaitBackgroundInterval(ctx context.Context, interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
