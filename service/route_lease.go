package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis/v8"
)

var (
	ErrRouteLeaseUnavailable = errors.New("route lease redis is unavailable")
	ErrRouteLeaseConflict    = errors.New("route lease is already owned")
	ErrRouteLeaseCapacity    = errors.New("route lease capacity is full")
	ErrRouteLeaseOwnership   = errors.New("route lease ownership mismatch")
	ErrRouteLeaseRuntime     = errors.New("route lease runtime state changed")
)

const (
	RouteLeaseStateAcquired      = "acquired"
	RouteLeaseStateReleased      = "released"
	RouteLeaseStateReleaseFailed = "release_failed"
	RouteLeaseStateRenewalFailed = "renewal_failed"
	RouteLeaseFailureCode        = "route_lease_failed"
)

const routeLeasePrefix = "route:lease:v1"

var routeLeaseMetrics struct {
	AcquireFailures atomic.Uint64
	RenewFailures   atomic.Uint64
	ReleaseFailures atomic.Uint64
}

// RouteLeaseMetricsSnapshot contains low-cardinality admission diagnostics.
// These counters are process-local observability only; Redis remains the
// source of truth for lease ownership and capacity.
type RouteLeaseMetricsSnapshot struct {
	AcquireFailures uint64 `json:"route_lease_acquire_failure_total"`
	RenewFailures   uint64 `json:"route_lease_renew_failure_total"`
	ReleaseFailures uint64 `json:"route_lease_release_failure_total"`
}

func RouteLeaseMetrics() RouteLeaseMetricsSnapshot {
	return RouteLeaseMetricsSnapshot{
		AcquireFailures: routeLeaseMetrics.AcquireFailures.Load(),
		RenewFailures:   routeLeaseMetrics.RenewFailures.Load(),
		ReleaseFailures: routeLeaseMetrics.ReleaseFailures.Load(),
	}
}

type RouteLeaseResource struct {
	Key      string
	Capacity int
}

type RouteLease struct {
	LeaseID   string
	RequestID string
	ChannelID int
	Resources []RouteLeaseResource
	ExpiresAt time.Time
}

func UserRouteLeaseKey(userID int) string {
	return fmt.Sprintf("%s:user:%d", routeLeasePrefix, userID)
}

func TokenRouteLeaseKey(tokenID int) string {
	return fmt.Sprintf("%s:token:%d", routeLeasePrefix, tokenID)
}

func ChannelModelRouteLeaseKey(channelID int, canonicalModel string) string {
	return fmt.Sprintf("%s:channel:%d:model:%s", routeLeasePrefix, channelID, canonicalModel)
}

func AcquireRouteLease(ctx context.Context, client *redis.Client, requestID, leaseID string, ttl time.Duration, resources []RouteLeaseResource) (RouteLease, error) {
	if client == nil || strings.TrimSpace(requestID) == "" || strings.TrimSpace(leaseID) == "" || ttl <= 0 || len(resources) == 0 {
		routeLeaseMetrics.AcquireFailures.Add(1)
		return RouteLease{}, ErrRouteLeaseUnavailable
	}
	keys := make([]string, 0, len(resources)+1)
	args := make([]interface{}, 0, 3+len(resources))
	seenResources := make(map[string]struct{}, len(resources))
	for _, resource := range resources {
		if strings.TrimSpace(resource.Key) == "" || resource.Capacity <= 0 {
			routeLeaseMetrics.AcquireFailures.Add(1)
			return RouteLease{}, ErrRouteLeaseUnavailable
		}
		if _, exists := seenResources[resource.Key]; exists {
			routeLeaseMetrics.AcquireFailures.Add(1)
			return RouteLease{}, ErrRouteLeaseUnavailable
		}
		seenResources[resource.Key] = struct{}{}
		keys = append(keys, resource.Key)
		args = append(args, resource.Capacity)
	}
	metaKey := routeLeaseMetaKey(leaseID)
	requestKey := routeLeaseRequestKey(requestID)
	keys = append(keys, metaKey, requestKey)
	args = append(args, leaseID, requestID, ttl.Milliseconds())
	result, err := routeLeaseAcquireScript.Run(ctx, client, keys, args...).Int()
	if err != nil {
		routeLeaseMetrics.AcquireFailures.Add(1)
		return RouteLease{}, fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	switch result {
	case 0:
		routeLeaseMetrics.AcquireFailures.Add(1)
		return RouteLease{}, ErrRouteLeaseConflict
	case -1:
		routeLeaseMetrics.AcquireFailures.Add(1)
		return RouteLease{}, ErrRouteLeaseCapacity
	}
	return RouteLease{LeaseID: leaseID, RequestID: requestID, Resources: resources, ExpiresAt: time.Now().Add(ttl)}, nil
}

func ReleaseRouteLease(ctx context.Context, client *redis.Client, lease RouteLease) error {
	if client == nil || strings.TrimSpace(lease.LeaseID) == "" || strings.TrimSpace(lease.RequestID) == "" || len(lease.Resources) == 0 {
		routeLeaseMetrics.ReleaseFailures.Add(1)
		return ErrRouteLeaseUnavailable
	}
	keys := make([]string, 0, len(lease.Resources)+2)
	for _, resource := range lease.Resources {
		keys = append(keys, resource.Key)
	}
	keys = append(keys, routeLeaseMetaKey(lease.LeaseID), routeLeaseRequestKey(lease.RequestID))
	result, err := routeLeaseReleaseScript.Run(ctx, client, keys, lease.LeaseID, lease.RequestID).Int()
	if err != nil {
		routeLeaseMetrics.ReleaseFailures.Add(1)
		return fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	if result == 0 {
		routeLeaseMetrics.ReleaseFailures.Add(1)
		return ErrRouteLeaseOwnership
	}
	return nil
}

func RenewRouteLease(ctx context.Context, client *redis.Client, lease RouteLease, ttl time.Duration) (time.Time, error) {
	if client == nil || strings.TrimSpace(lease.LeaseID) == "" || strings.TrimSpace(lease.RequestID) == "" || ttl <= 0 {
		routeLeaseMetrics.RenewFailures.Add(1)
		return time.Time{}, ErrRouteLeaseUnavailable
	}
	keys := make([]string, 0, len(lease.Resources)+2)
	for _, resource := range lease.Resources {
		keys = append(keys, resource.Key)
	}
	keys = append(keys, routeLeaseMetaKey(lease.LeaseID), routeLeaseRequestKey(lease.RequestID))
	result, err := routeLeaseRenewScript.Run(ctx, client, keys, lease.LeaseID, lease.RequestID, ttl.Milliseconds()).Int()
	if err != nil {
		routeLeaseMetrics.RenewFailures.Add(1)
		return time.Time{}, fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	if result == 0 {
		routeLeaseMetrics.RenewFailures.Add(1)
		return time.Time{}, ErrRouteLeaseOwnership
	}
	return time.Now().Add(ttl), nil
}

type RouteLeaseRuntimeState struct {
	ChannelEnabled    bool
	HealthEpoch       int64
	CapabilityVersion int64
	PolicyEnabled     bool
	PolicyVersion     int64
}

func RecheckRouteLeaseRuntime(expected, current RouteLeaseRuntimeState) error {
	if !expected.ChannelEnabled || !current.ChannelEnabled ||
		!expected.PolicyEnabled || !current.PolicyEnabled ||
		expected.PolicyVersion <= 0 || current.PolicyVersion <= 0 ||
		expected.PolicyVersion != current.PolicyVersion ||
		expected.HealthEpoch != current.HealthEpoch ||
		expected.CapabilityVersion != current.CapabilityVersion {
		return ErrRouteLeaseRuntime
	}
	return nil
}

func routeLeaseMetaKey(leaseID string) string {
	return fmt.Sprintf("%s:meta:%s", routeLeasePrefix, leaseID)
}

func routeLeaseRequestKey(requestID string) string {
	return fmt.Sprintf("%s:request:%s", routeLeasePrefix, requestID)
}

var routeLeaseAcquireScript = redis.NewScript(`
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
local lease_id = ARGV[#ARGV - 2]
local request_id = ARGV[#ARGV - 1]
local ttl_ms = tonumber(ARGV[#ARGV])
local meta_key = KEYS[#KEYS - 1]
local request_key = KEYS[#KEYS]
if redis.call('EXISTS', meta_key) == 1 or redis.call('EXISTS', request_key) == 1 then
  return 0
end
local expiry = now_ms + ttl_ms
for i = 1, #KEYS - 2 do
  local expired = redis.call('ZRANGEBYSCORE', KEYS[i], '-inf', now_ms)
  for _, member in ipairs(expired) do redis.call('ZREM', KEYS[i], member) end
  local capacity = tonumber(ARGV[i])
  if redis.call('ZCARD', KEYS[i]) >= capacity then return -1 end
end
for i = 1, #KEYS - 2 do redis.call('ZADD', KEYS[i], expiry, lease_id) end
redis.call('HSET', meta_key, 'lease_id', lease_id, 'request_id', request_id, 'expires_at', expiry)
redis.call('HSET', meta_key, 'resource_count', #KEYS - 2)
for i = 1, #KEYS - 2 do redis.call('HSET', meta_key, 'resource:' .. i, KEYS[i]) end
redis.call('PEXPIRE', meta_key, ttl_ms)
redis.call('SET', request_key, lease_id, 'PX', ttl_ms)
return 1
`)

var routeLeaseReleaseScript = redis.NewScript(`
local meta_key = KEYS[#KEYS - 1]
local request_key = KEYS[#KEYS]
local lease_id = ARGV[1]
local request_id = ARGV[2]
if redis.call('HGET', meta_key, 'lease_id') ~= lease_id or redis.call('HGET', meta_key, 'request_id') ~= request_id then return 0 end
if redis.call('GET', request_key) ~= lease_id then return 0 end
if tonumber(redis.call('HGET', meta_key, 'resource_count') or '0') ~= #KEYS - 2 then return 0 end
for i = 1, #KEYS - 2 do if redis.call('HGET', meta_key, 'resource:' .. i) ~= KEYS[i] then return 0 end end
for i = 1, #KEYS - 2 do redis.call('ZREM', KEYS[i], lease_id) end
redis.call('DEL', meta_key)
redis.call('DEL', request_key)
return 1
`)

var routeLeaseRenewScript = redis.NewScript(`
local meta_key = KEYS[#KEYS - 1]
local request_key = KEYS[#KEYS]
local lease_id = ARGV[1]
local request_id = ARGV[2]
local ttl_ms = tonumber(ARGV[3])
if redis.call('HGET', meta_key, 'lease_id') ~= lease_id or redis.call('HGET', meta_key, 'request_id') ~= request_id then return 0 end
if redis.call('GET', request_key) ~= lease_id then return 0 end
if tonumber(redis.call('HGET', meta_key, 'resource_count') or '0') ~= #KEYS - 2 then return 0 end
for i = 1, #KEYS - 2 do if redis.call('HGET', meta_key, 'resource:' .. i) ~= KEYS[i] then return 0 end end
local now_parts = redis.call('TIME')
local now_ms = tonumber(now_parts[1]) * 1000 + math.floor(tonumber(now_parts[2]) / 1000)
redis.call('HSET', meta_key, 'expires_at', now_ms + ttl_ms)
redis.call('PEXPIRE', meta_key, ttl_ms)
redis.call('PEXPIRE', request_key, ttl_ms)
for i = 1, #KEYS - 2 do redis.call('ZADD', KEYS[i], now_ms + ttl_ms, lease_id) end
return 1
`)
