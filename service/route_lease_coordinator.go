package service

import (
	"context"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
)

type RouteRuntimeProbe func(context.Context) (RouteLeaseRuntimeState, error)

// ExecuteWithConfiguredRouteLease owns one route attempt. The callback is
// invoked only after the lease and runtime state pass; release is attempted on
// every exit path and release failures are returned. A missing policy, Redis
// outage, or stale runtime snapshot therefore fails the new route without an
// implicit unlimited-concurrency fallback.
func ExecuteWithConfiguredRouteLease(
	ctx context.Context,
	requestID string,
	channelID, userID, tokenID int,
	requestModel string,
	ttl time.Duration,
	expected RouteLeaseRuntimeState,
	probe RouteRuntimeProbe,
	execute func(context.Context, RouteLease, model.ChannelRoutePolicy) error,
) (resultErr error) {
	if probe == nil || execute == nil {
		return ErrRouteLeaseRuntime
	}
	lease, policy, err := AcquireConfiguredRouteLease(ctx, requestID, channelID, userID, tokenID, requestModel, ttl)
	if err != nil {
		return err
	}
	defer func() {
		releaseErr := ReleaseConfiguredRouteLease(context.Background(), lease)
		if releaseErr != nil {
			resultErr = errors.Join(resultErr, releaseErr)
		}
	}()
	if !expected.PolicyEnabled || expected.PolicyVersion <= 0 ||
		!policy.Enabled || expected.PolicyVersion != policy.Version {
		return ErrRouteLeaseRuntime
	}
	current, err := probe(ctx)
	if err != nil {
		return err
	}
	if err := RecheckRouteLeaseRuntime(expected, current); err != nil {
		return err
	}
	return execute(ctx, lease, policy)
}

// StartRouteLeaseRenewal keeps a streaming lease alive until the request is
// cancelled or Stop is called. Renewal failures are reported on Done; the
// caller decides whether to terminate the upstream stream.
type RouteLeaseRenewal struct {
	Done <-chan error
	Stop func()
}

func StartRouteLeaseRenewal(ctx context.Context, client *redis.Client, lease RouteLease, interval, ttl time.Duration) RouteLeaseRenewal {
	if client == nil {
		done := make(chan error, 1)
		done <- ErrRouteLeaseUnavailable
		close(done)
		return RouteLeaseRenewal{Done: done, Stop: func() {}}
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
	child, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-child.Done():
				return
			case <-ticker.C:
				if _, err := RenewRouteLease(child, client, lease, ttl); err != nil {
					done <- err
					return
				}
			}
		}
	}()
	return RouteLeaseRenewal{Done: done, Stop: cancel}
}
