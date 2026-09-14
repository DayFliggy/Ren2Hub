package service

import (
	"context"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
)

// StartRouteLeaseRenewal keeps a streaming lease alive until the request is
// cancelled or Stop is called. Renewal failures are reported on Done; the
// caller decides whether to terminate the upstream stream.
type RouteLeaseRenewal struct {
	Done    <-chan error
	Stop    func()
	Failure func() error
}

func StartRouteLeaseRenewal(ctx context.Context, client *redis.Client, lease RouteLease, interval, ttl time.Duration, renewOwned ...func(context.Context) error) RouteLeaseRenewal {
	if client == nil {
		done := make(chan error, 1)
		done <- ErrRouteLeaseUnavailable
		close(done)
		return RouteLeaseRenewal{Done: done, Stop: func() {}, Failure: func() error { return ErrRouteLeaseUnavailable }}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if interval <= 0 {
		interval = ttl / 3
	}
	if interval <= 0 {
		interval = time.Second
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return startRouteRenewal(ctx, interval, func(ctx context.Context) error {
		if _, err := RenewRouteLease(ctx, client, lease, ttl); err != nil {
			return err
		}
		for _, renew := range renewOwned {
			if err := renew(ctx); err != nil {
				return err
			}
		}
		return nil
	})
}

func startRouteRenewal(ctx context.Context, interval time.Duration, renew func(context.Context) error) RouteLeaseRenewal {
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
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-ticker.C:
				if err := renew(child); err != nil {
					if child.Err() != nil {
						return
					}
					failureMu.Lock()
					failureErr = err
					failureMu.Unlock()
					done <- err
					return
				}
			}
		}
	}()
	return RouteLeaseRenewal{Done: done, Stop: cancel, Failure: failure}
}
