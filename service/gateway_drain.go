package service

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

type gatewayActiveRequest struct {
	cancel      context.CancelFunc
	connections []net.Conn
	tracker     *gatewayRequestTracker
	stopping    bool
}
type gatewayRequestKey struct{}

type gatewayRequestTracker struct {
	sync.Mutex
	active   map[*gatewayActiveRequest]struct{}
	draining bool
	changed  chan struct{}
}

func newGatewayRequestTracker() *gatewayRequestTracker {
	return &gatewayRequestTracker{active: make(map[*gatewayActiveRequest]struct{}), changed: make(chan struct{})}
}

var gatewayRequests = newGatewayRequestTracker()

func TrackGatewayRequest(ctx context.Context) (context.Context, func(), error) {
	return gatewayRequests.track(ctx)
}

func (tracker *gatewayRequestTracker) track(ctx context.Context) (context.Context, func(), error) {
	tracker.Lock()
	defer tracker.Unlock()
	if tracker.draining {
		return ctx, nil, ErrGatewayDraining
	}
	if err := ctx.Err(); err != nil {
		return ctx, nil, err
	}
	child, cancel := context.WithCancel(ctx)
	r := &gatewayActiveRequest{cancel: cancel, tracker: tracker}
	tracker.active[r] = struct{}{}
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			tracker.Lock()
			delete(tracker.active, r)
			close(tracker.changed)
			tracker.changed = make(chan struct{})
			tracker.Unlock()
		})
	}
	return context.WithValue(child, gatewayRequestKey{}, r), finish, nil
}

func TrackGatewayConnection(ctx context.Context, conn net.Conn) {
	r, ok := ctx.Value(gatewayRequestKey{}).(*gatewayActiveRequest)
	if !ok || conn == nil {
		return
	}
	r.tracker.Lock()
	_, active := r.tracker.active[r]
	stopped := !active || r.stopping || ctx.Err() != nil
	if !stopped {
		r.connections = append(r.connections, conn)
	}
	r.tracker.Unlock()
	if stopped {
		_ = conn.Close()
	}
}

func BeginGatewayDrain() {
	gatewayRequests.Lock()
	gatewayRequests.draining = true
	gatewayRequests.Unlock()
	gatewayAdmission.BeginDrain()
}

// WaitGatewayDrain includes hijacked connections, which http.Server.Shutdown
// does not wait for. Forced cancellation leaves a bounded cleanup window for
// request owners to release permits and settle reservations before dependencies close.
func WaitGatewayDrain(ctx context.Context) error {
	return gatewayRequests.drain(ctx, 5*time.Second)
}

func (tracker *gatewayRequestTracker) wait(ctx context.Context) error {
	for {
		tracker.Lock()
		empty, changed := len(tracker.active) == 0, tracker.changed
		tracker.Unlock()
		if empty {
			return nil
		}
		select {
		case <-changed:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (tracker *gatewayRequestTracker) drain(ctx context.Context, cleanup time.Duration) error {
	if err := tracker.wait(ctx); err == nil {
		return nil
	}
	tracker.Lock()
	var cancels []context.CancelFunc
	var connections []net.Conn
	for request := range tracker.active {
		request.stopping = true
		cancels = append(cancels, request.cancel)
		connections = append(connections, request.connections...)
	}
	tracker.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	for _, conn := range connections {
		_ = conn.Close()
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanup)
	defer cancel()
	if err := tracker.wait(cleanupCtx); err != nil {
		return errors.Join(ctx.Err(), errors.New("gateway request cleanup deadline exceeded"))
	}
	return ctx.Err()
}
