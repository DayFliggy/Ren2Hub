package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// Redis Lua numbers represent these monotonically increasing versions exactly.
const maxSettlementVersion int64 = 1<<53 - 1

var ErrTaskQuotaCachePending = errors.New("task quota cache synchronization pending")

func taskSettlementVersionKey(kind string, id int) string {
	return fmt.Sprintf("quota:settlement:%s:%d", kind, id)
}

// Called while the corresponding database row is locked. The persistent floor
// blocks reservations before commit, and also rejects arbitrarily delayed fills.
// If the transaction rolls back, reconciliation takes the same row lock before
// restoring the durable version. No hash or unflushed quota delta is deleted.
func fenceTaskSettlementCache(ctx context.Context, kind string, id int, version int64) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RDB.Set(ctx, taskSettlementVersionKey(kind, id), version, 0).Err()
}

func taskSettlementCacheCurrent(ctx context.Context, kind string, id int, version int64) (bool, error) {
	if !common.RedisEnabled {
		return true, nil
	}
	floor, err := common.RDB.Get(ctx, taskSettlementVersionKey(kind, id)).Int64()
	if errors.Is(err, redis.Nil) {
		return true, nil
	}
	return version == floor, err
}

func SyncTaskSettlementCache(ctx context.Context, taskID int64) error {
	var receipt TaskSettlement
	if err := DB.WithContext(ctx).First(&receipt, "task_id = ?", taskID).Error; err != nil {
		return err
	}
	if !receipt.Applied {
		return errors.New("task settlement has not committed")
	}
	if receipt.CacheSynced {
		return nil
	}
	if receipt.UserVersion > 0 {
		if err := reconcileTaskQuotaCache(ctx, "user", receipt.UserID); err != nil {
			return err
		}
	}
	if receipt.TokenVersion > 0 {
		if err := reconcileTaskQuotaCache(ctx, "token", receipt.TokenID); err != nil {
			return err
		}
	}
	return DB.WithContext(ctx).Model(&receipt).Update("cache_synced", true).Error
}

func reconcileTaskQuotaCache(ctx context.Context, kind string, id int) error {
	if !common.RedisEnabled {
		return nil
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version int64
		var cacheKey string
		switch kind {
		case "user":
			var user User
			if err := lockForUpdate(tx.Unscoped()).Select("id", "settlement_version").First(&user, id).Error; err != nil {
				return err
			}
			version, cacheKey = user.SettlementVersion, getUserCacheKey(id)
		case "token":
			var token Token
			if err := lockForUpdate(tx.Unscoped()).Select("id", "key", "settlement_version").First(&token, id).Error; err != nil {
				return err
			}
			version, cacheKey = token.SettlementVersion, getTokenCacheKey(token.Key)
		default:
			return errors.New("invalid task quota resource")
		}
		values, err := common.RDB.HMGet(ctx, cacheKey, "Id", "SettlementVersion").Result()
		if err != nil {
			return err
		}
		cachedVersion := int64(0)
		if values[1] != nil {
			cachedVersion, err = strconv.ParseInt(fmt.Sprint(values[1]), 10, 64)
			if err != nil {
				return err
			}
		}
		if cachedVersion > version {
			return errors.New("task quota cache is newer than durable accounting")
		}
		var delta int64
		if values[0] != nil && cachedVersion < version {
			// Identifiers are chosen only from the closed resource switch above.
			rows, err := tx.Model(&TaskSettlement{}).Select("delta", kind+"_version").
				Where(kind+"_id = ? AND "+kind+"_version > ? AND "+kind+"_version <= ? AND applied = ?", id, cachedVersion, version, true).
				Order(kind + "_version").Rows()
			if err != nil {
				return err
			}
			defer rows.Close()
			expected := cachedVersion
			for rows.Next() {
				var amount int64
				var actualVersion int64
				if err := rows.Scan(&amount, &actualVersion); err != nil {
					return err
				}
				expected++
				if actualVersion != expected {
					return errors.New("task quota journal version gap")
				}
				delta -= amount
				if delta < -maxSettlementVersion || delta > maxSettlementVersion {
					return errors.New("task quota cache adjustment overflow")
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if expected != version {
				return errors.New("task quota journal is incomplete")
			}
		}
		const script = `
local present = redis.call('HEXISTS', KEYS[1], 'Id') == 1
local current = tonumber(redis.call('HGET', KEYS[1], 'SettlementVersion') or '0')
if present and (ARGV[5] == '0' or current ~= tonumber(ARGV[1])) then return 0 end
if present then
  local field = ARGV[4] == 'user' and 'Quota' or 'RemainQuota'
  if redis.call('HEXISTS', KEYS[1], field) == 0 then return 0 end
  local value = tonumber(redis.call('HGET', KEYS[1], field)) + tonumber(ARGV[3])
  if value < -2147483648 or value > 2147483647 then return -1 end
  if ARGV[4] == 'token' then
    local used = tonumber(redis.call('HGET', KEYS[1], 'UsedQuota') or '0') - tonumber(ARGV[3])
    if used < -2147483648 or used > 2147483647 then return -1 end
    redis.call('HINCRBY', KEYS[1], 'UsedQuota', -tonumber(ARGV[3]))
  end
  redis.call('HINCRBY', KEYS[1], field, ARGV[3])
  redis.call('HSET', KEYS[1], 'SettlementVersion', ARGV[2])
end
redis.call('SET', KEYS[2], ARGV[2])
return 1`
		present := 0
		if values[0] != nil {
			present = 1
		}
		result, err := common.RDB.Eval(ctx, script, []string{cacheKey, taskSettlementVersionKey(kind, id)}, cachedVersion, version, delta, kind, present).Int()
		if err != nil {
			return err
		}
		if result != 1 {
			return ErrTaskQuotaCachePending
		}
		return nil
	})
}
