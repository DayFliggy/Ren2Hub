package model

import (
	"context"

	"github.com/QuantumNous/new-api/common"
)

// Direct debits must commit inside the same boundary as reservations and cache
// hydration. A committed database change stays successful if publication fails.
func updateQuotaDirect(kind, id int, key string, write func() error) error {
	unlock := lockQuotaReservation(kind, id)
	defer unlock()
	if err := write(); err != nil {
		return err
	}
	updateQuotaCacheLocked(kind, id, key, 0, true)
	return nil
}

// All balance publication owns the reservation lock. Queued mutations apply a
// delta; committed mutations publish a fresh authoritative balance. Redis errors
// may follow a successful script, so neither path retries an uncertain delta.
func updateQuotaCacheLocked(kind, id int, key string, delta int, fromDatabase bool) {
	if !common.RedisEnabled {
		return
	}
	var err error
	if fromDatabase {
		err = replaceQuotaCacheFromDB(kind, id, key)
	} else if kind == BatchUpdateTypeUserQuota {
		_, err = cacheApplyUserQuotaDelta(id, int64(delta))
	} else {
		_, err = cacheApplyTokenQuotaDelta(id, key, int64(delta))
	}
	if err != nil {
		quotaCacheNeedsReload[kind].Store(id, true)
		common.SysLog(common.LogText("failed to synchronize quota cache: type=%d id=%d error=%v", kind, id, err))
		return
	}
	if fromDatabase {
		quotaCacheNeedsReload[kind].Delete(id)
	}
}

func replaceQuotaCacheFromDB(kind, id int, key string) error {
	// Persist outstanding reservations before taking the snapshot, using the
	// existing detach/merge rule instead of reconstructing a second balance.
	if err := flushPendingQuota(kind, id); err != nil {
		return err
	}
	if kind == BatchUpdateTypeUserQuota {
		var user User
		if err := DB.Select("quota").First(&user, id).Error; err != nil {
			return err
		}
		const script = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[2])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return 0
end
redis.call('HSET', KEYS[1], 'Quota', ARGV[3])
return 1`
		return common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(id)}, id, userCacheSchemaVersion, user.Quota).Err()
	}
	var token Token
	if err := DB.Select("remain_quota", "used_quota", "accessed_time").First(&token, id).Error; err != nil {
		return err
	}
	const script = `
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[1])
  or redis.call('HEXISTS', KEYS[1], 'RemainQuota') == 0
  or redis.call('HEXISTS', KEYS[1], 'UsedQuota') == 0 then
  return 0
end
redis.call('HSET', KEYS[1], 'RemainQuota', ARGV[2], 'UsedQuota', ARGV[3], 'AccessedTime', ARGV[4])
return 1`
	return common.RDB.Eval(context.Background(), script, []string{getTokenCacheKey(key)}, id, token.RemainQuota, token.UsedQuota, token.AccessedTime).Err()
}
