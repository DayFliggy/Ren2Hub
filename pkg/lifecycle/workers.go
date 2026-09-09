package lifecycle

import (
	"context"
	"sync"
)

// Workers seals admission before waiting, so Add cannot race with shutdown.
// A stopped worker set is single-use and cannot restart accidentally.
type Workers struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	stopped bool
	wg      sync.WaitGroup
	done    chan struct{}
}

func (w *Workers) Go(run func(context.Context)) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return false
	}
	if w.ctx == nil {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	w.wg.Add(1)
	go func() { defer w.wg.Done(); run(w.ctx) }()
	return true
}
func (w *Workers) Stop(ctx context.Context) error {
	w.mu.Lock()
	if !w.stopped {
		w.stopped = true
		w.done = make(chan struct{})
		if w.cancel != nil {
			w.cancel()
		}
		go func() { w.wg.Wait(); close(w.done) }()
	}
	done := w.done
	w.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Accepting reports whether callers may enqueue more work. Actual goroutine
// admission must still use Go, which rechecks under the shutdown mutex.
func (w *Workers) Accepting() bool { w.mu.Lock(); defer w.mu.Unlock(); return !w.stopped }
