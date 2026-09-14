package service

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

const gatewayQueuePollInterval = 20 * time.Millisecond

func AcquireQueuedRouteLease(ctx context.Context, client *redis.Client, requestID, leaseID string, ttl time.Duration, resource RouteLeaseResource, queueSize int, budget *GatewayWaitBudget, refreshCapacity ...func(context.Context) (int, error)) (lease RouteLease, resultErr error) {
	defer func() {
		if resultErr != nil {
			routeLeaseMetrics.AcquireFailures.Add(1)
		}
	}()
	if client == nil || requestID == "" || leaseID == "" || ttl <= 0 || resource.Key == "" || resource.Capacity < 0 || queueSize < 0 {
		return RouteLease{}, ErrRouteLeaseUnavailable
	}
	if err := ctx.Err(); err != nil {
		return RouteLease{}, err
	}
	remaining := budget.Remaining()
	started := time.Now()
	queued := false
	counter := gatewayQueueCounter(resource.Key)
	keys := []string{resource.Key, routeLeaseMetaKey(leaseID), routeLeaseRequestKey(requestID), resource.Key + ":queue", resource.Key + ":queue_expiry", resource.Key + ":sequence"}
	defer func() {
		if queued {
			budget.consume(time.Since(started))
			if counter != nil {
				counter.active.Add(-1)
				counter.waitMS.Add(time.Since(started).Milliseconds())
				if resultErr == nil {
					counter.completed.Add(1)
				} else {
					counter.failed.Add(1)
				}
			}
		}
	}()
	cleanup := func(cause error) (RouteLease, error) {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_, err := gatewayQueueCancelScript.Run(cleanupCtx, client, keys[3:5], leaseID).Result()
		return RouteLease{}, errors.Join(cause, err)
	}
	for {
		waitMS := max((remaining - time.Since(started)).Milliseconds(), 0)
		if queued && waitMS <= 0 {
			return cleanup(ErrGatewayWaitExpired)
		}
		if !queued {
			waitMS = max(remaining.Milliseconds(), 0)
		}
		if queued && len(refreshCapacity) > 0 {
			capacity, err := refreshCapacity[0](ctx)
			if err != nil {
				return cleanup(err)
			}
			if capacity < 0 {
				return cleanup(ErrRouteLeaseRuntime)
			}
			resource.Capacity = capacity
		}
		result, err := gatewayQueuedAcquireScript.Run(ctx, client, keys, leaseID, requestID, ttl.Milliseconds(), resource.Capacity, queueSize, waitMS, queued).Int()
		if err != nil {
			return cleanup(errors.Join(ErrRouteLeaseUnavailable, err))
		}
		switch result {
		case 0:
			if !queued && counter != nil {
				counter.active.Add(1)
				counter.entered.Add(1)
			}
		case 1:
			return RouteLease{LeaseID: leaseID, RequestID: requestID, Resources: []RouteLeaseResource{resource}, ExpiresAt: time.Now().Add(ttl)}, nil
		case -1:
			return cleanup(ErrGatewayQueueFull)
		case -2:
			return cleanup(ErrRouteLeaseConflict)
		case -3:
			return cleanup(ErrGatewayWaitExpired)
		case -4:
			return cleanup(ErrRouteLeaseCapacity)
		default:
			return cleanup(ErrRouteLeaseUnavailable)
		}
		queued = true
		if waitMS <= 0 {
			return cleanup(ErrGatewayWaitExpired)
		}
		timer := time.NewTimer(min(gatewayQueuePollInterval, time.Duration(waitMS)*time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return cleanup(ctx.Err())
		case <-timer.C:
		}
	}
}

var gatewayQueueCancelScript = redis.NewScript(`
redis.call('ZREM', KEYS[1], ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
return 1
`)

// The resource identity never changes on configuration updates. Capacity is a
// current admission bound, while existing occupants retain their same leases.
var gatewayQueuedAcquireScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1]) * 1000 + math.floor(tonumber(parts[2]) / 1000)
local id, request_id = ARGV[1], ARGV[2]
local ttl, capacity, queue_size, wait = tonumber(ARGV[3]), tonumber(ARGV[4]), tonumber(ARGV[5]), tonumber(ARGV[6])
if redis.call('EXISTS', KEYS[2]) == 1 or redis.call('EXISTS', KEYS[3]) == 1 then return -2 end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
local expired = redis.call('ZRANGEBYSCORE', KEYS[5], '-inf', now)
for _, ticket in ipairs(expired) do
  redis.call('ZREM', KEYS[4], ticket)
  redis.call('ZREM', KEYS[5], ticket)
end
local position = redis.call('ZRANK', KEYS[4], id)
if ARGV[7] == '1' and not position then return -3 end
local waiting = redis.call('ZCARD', KEYS[4])
if (capacity == 0 or redis.call('ZCARD', KEYS[1]) < capacity) and (waiting == 0 or position == 0) then
  redis.call('ZREM', KEYS[4], id)
  redis.call('ZREM', KEYS[5], id)
  redis.call('ZADD', KEYS[1], now + ttl, id)
  redis.call('HSET', KEYS[2], 'lease_id', id, 'request_id', request_id, 'expires_at', now + ttl, 'resource_count', 1, 'resource:1', KEYS[1])
  redis.call('PEXPIRE', KEYS[2], ttl)
  redis.call('SET', KEYS[3], id, 'PX', ttl)
  return 1
end
if wait <= 0 then return -4 end
if not position then
  if queue_size == 0 then return -4 end
  if waiting >= queue_size then return -1 end
  local sequence = redis.call('INCR', KEYS[6])
  redis.call('ZADD', KEYS[4], sequence, id)
  redis.call('ZADD', KEYS[5], now + wait, id)
  redis.call('PEXPIRE', KEYS[4], 120000)
  redis.call('PEXPIRE', KEYS[5], 120000)
  redis.call('PEXPIRE', KEYS[6], 240000)
end
return 0
`)
