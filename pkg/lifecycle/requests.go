package lifecycle

import (
	"context"
	"sync"
)

// Requests includes hijacked connections, which http.Server.Shutdown does not
// wait for. Sealing rejects new work while allowing accepted requests to drain.
type Requests struct {
	mu     sync.Mutex
	next   uint64
	active map[uint64]context.CancelFunc
	sealed bool
	done   chan struct{}
}

func (r *Requests) Begin(parent context.Context) (context.Context, func(), bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sealed {
		return parent, func() {}, false
	}
	if r.active == nil {
		r.active = make(map[uint64]context.CancelFunc)
	}
	r.next++
	id := r.next
	ctx, cancel := context.WithCancel(parent)
	r.active[id] = cancel
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			cancel()
			r.mu.Lock()
			defer r.mu.Unlock()
			delete(r.active, id)
			if r.sealed && len(r.active) == 0 {
				close(r.done)
			}
		})
	}, true
}

func (r *Requests) Seal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.sealed {
		r.sealed = true
		r.done = make(chan struct{})
		if len(r.active) == 0 {
			close(r.done)
		}
	}
}

func (r *Requests) Stop(ctx context.Context) error {
	r.Seal()
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		r.mu.Lock()
		for _, cancel := range r.active {
			cancel()
		}
		r.mu.Unlock()
		return ctx.Err()
	}
}
