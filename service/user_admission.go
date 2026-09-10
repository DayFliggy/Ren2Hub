package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/google/uuid"
)

var (
	ErrUserRPM          = errors.New("user_rpm_exceeded")
	ErrUserQueueFull    = errors.New("user_queue_full")
	ErrUserQueueTimeout = errors.New("user_queue_timeout")
)

const UserAdmissionLeaseTTL = 120 * time.Second
const UserAdmissionRenewInterval = 30 * time.Second

// Every admitted request, including an unlimited user, has an active Redis
// member. A request owns its slot until all billing and response cleanup ends.
func AcquireUserAdmission(ctx context.Context, userID int, requestID string) (RouteLease, error) {
	if userID <= 0 || requestID == "" || !common.RedisEnabled || common.RDB == nil {
		return RouteLease{}, ErrRouteLeaseUnavailable
	}
	leaseID := uuid.NewString()
	deadline := time.Now().Add(30 * time.Second)
	var result RouteLease
	admitted := false
	defer func() {
		if admitted {
			return
		}
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cancelUserAdmissionTicket(cleanup, common.RDB, userID, leaseID); err != nil {
			routeLeaseMetrics.ReleaseFailures.Add(1)
			common.SysError("user admission ticket cleanup failed: " + err.Error())
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if time.Now().After(deadline) {
			return RouteLease{}, ErrUserQueueTimeout
		}
		status := 0
		err := model.WithUserAdmissionPolicy(ctx, userID, func(policy model.EffectiveUserLimits) error {
			var err error
			status, result, err = userAdmissionStep(ctx, common.RDB, userID, requestID, leaseID, policy)
			return err
		})
		if err != nil {
			if status == 1 {
				cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				releaseErr := ReleaseRouteLease(cleanup, common.RDB, result)
				cancel()
				err = errors.Join(err, releaseErr)
			}
			return RouteLease{}, err
		}
		switch status {
		case 1:
			admitted = true
			return result, nil
		case -1:
			return RouteLease{}, ErrUserQueueFull
		case -2:
			return RouteLease{}, ErrUserRPM
		case -3:
			return RouteLease{}, ErrRouteLeaseConflict
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return RouteLease{}, ErrUserQueueTimeout
		}
		wait := min(100*time.Millisecond, remaining)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return RouteLease{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func userAdmissionKeys(userID int) (string, string, string) {
	base := fmt.Sprintf("route:admission:v1:user:%d", userID)
	return base + ":queue", base + ":expires", base + ":sequence"
}

func cancelUserAdmissionTicket(ctx context.Context, client *redis.Client, userID int, ticket string) error {
	queue, expires, _ := userAdmissionKeys(userID)
	return client.Eval(ctx, `redis.call('ZREM',KEYS[1],ARGV[1]); redis.call('ZREM',KEYS[2],ARGV[1]); return 1`, []string{queue, expires}, ticket).Err()
}

func userAdmissionStep(ctx context.Context, client *redis.Client, userID int, requestID, leaseID string, policy model.EffectiveUserLimits) (int, RouteLease, error) {
	owner := RouteLeaseOwner{RequestID: requestID, Scope: RouteLeaseScopeRequest}
	queue, expires, sequence := userAdmissionKeys(userID)
	resource := UserRouteLeaseKey(userID)
	keys := []string{resource, routeLeaseMetaKey(leaseID), routeLeaseOwnerKey(owner), queue, expires, sequence,
		fmt.Sprintf("route:admission:v1:user:%d:rpm", userID), routeLeaseOwnerKey(owner) + ":rpm"}
	status, err := userAdmissionScript.Run(ctx, client, keys, leaseID, requestID, policy.Concurrency, policy.RPM,
		UserAdmissionLeaseTTL.Milliseconds(), 30000, 20, policy.UserPolicyVersion, policy.ClassPolicyVersion).Int()
	if err != nil {
		return 0, RouteLease{}, fmt.Errorf("%w: %v", ErrRouteLeaseUnavailable, err)
	}
	lease := RouteLease{LeaseID: leaseID, RequestID: requestID, Scope: RouteLeaseScopeRequest,
		Resources: []RouteLeaseResource{{Key: resource, Capacity: policy.Concurrency}}, ExpiresAt: time.Now().Add(UserAdmissionLeaseTTL)}
	return status, lease, nil
}

var userAdmissionScript = redis.NewScript(`
local parts = redis.call('TIME')
local now = tonumber(parts[1])*1000 + math.floor(tonumber(parts[2])/1000)
local minute = math.floor(tonumber(parts[1])/60)
local lease,request = ARGV[1],ARGV[2]
local limit,rpm,ttl,wait,maxqueue = tonumber(ARGV[3]),tonumber(ARGV[4]),tonumber(ARGV[5]),tonumber(ARGV[6]),tonumber(ARGV[7])
if redis.call('EXISTS',KEYS[3]) == 1 then return -3 end
local rpm_checked = redis.call('GET',KEYS[8])
if rpm_checked == 'rejected' then return -2 end
if not rpm_checked then
  local bucket = KEYS[7] .. ':' .. minute
  local count = redis.call('INCR',bucket)
  redis.call('EXPIRE',bucket,120)
  if rpm > 0 and count > rpm then
    redis.call('SET',KEYS[8],'rejected','PX',ttl)
    return -2
  end
  redis.call('SET',KEYS[8],'counted','PX',ttl)
end
redis.call('ZREMRANGEBYSCORE',KEYS[1],'-inf',now)
local expired = redis.call('ZRANGEBYSCORE',KEYS[5],'-inf',now)
for _,ticket in ipairs(expired) do redis.call('ZREM',KEYS[4],ticket);redis.call('ZREM',KEYS[5],ticket) end
local head = redis.call('ZRANGE',KEYS[4],0,0)[1]
local can_enter = (not head or head == lease) and (limit == 0 or redis.call('ZCARD',KEYS[1]) < limit)
if can_enter then
  redis.call('ZREM',KEYS[4],lease);redis.call('ZREM',KEYS[5],lease)
  redis.call('ZADD',KEYS[1],now+ttl,lease)
  redis.call('HSET',KEYS[2],'lease_id',lease,'request_id',request,'expires_at',now+ttl,
    'resource_count',1,'resource:1',KEYS[1],'user_policy_version',ARGV[8],'class_policy_version',ARGV[9])
  redis.call('PEXPIRE',KEYS[2],ttl)
  redis.call('SET',KEYS[3],lease,'PX',ttl)
  return 1
end
if not redis.call('ZSCORE',KEYS[4],lease) then
  if redis.call('ZCARD',KEYS[4]) >= maxqueue then return -1 end
  local order = redis.call('INCR',KEYS[6])
  redis.call('ZADD',KEYS[4],order,lease)
  redis.call('ZADD',KEYS[5],now+wait,lease)
end
return 0
`)
