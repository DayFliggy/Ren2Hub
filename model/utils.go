package model

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

const (
	BatchUpdateTypeUserQuota = iota
	BatchUpdateTypeTokenQuota
	BatchUpdateTypeUsedQuota
	BatchUpdateTypeChannelUsedQuota
	BatchUpdateTypeRequestCount
	BatchUpdateTypeCount // if you add a new type, you need to add a new map and a new lock
)

var batchUpdateStores []map[int]int
var batchUpdateLocks []sync.Mutex

// quotaMutationLocks serialize a resource's cache-backed reservation, its
// queued database delta, and a durable recovery mutation. Stripes only trade
// a small amount of unrelated parallelism for bounded lock bookkeeping; they
// never change the resource key used for accounting.
const quotaMutationLockStripes = 257

var quotaMutationLocks [quotaMutationLockStripes]sync.Mutex
var quotaBatchMutationLock sync.Mutex

func quotaMutationLockIndex(resourceType, id int) int {
	if id < 0 {
		id = -id
	}
	return (id*31 + resourceType) % quotaMutationLockStripes
}

// lockQuotaMutationResources always locks stripes in ascending order so a
// recovery that needs both a user and a token cannot deadlock with another
// quota mutation.
func lockQuotaMutationResources(userID, tokenID int) func() {
	indexes := make([]int, 0, 2)
	if userID > 0 {
		indexes = append(indexes, quotaMutationLockIndex(BatchUpdateTypeUserQuota, userID))
	}
	if tokenID > 0 {
		indexes = append(indexes, quotaMutationLockIndex(BatchUpdateTypeTokenQuota, tokenID))
	}
	sort.Ints(indexes)
	unique := indexes[:0]
	for _, index := range indexes {
		if len(unique) == 0 || unique[len(unique)-1] != index {
			unique = append(unique, index)
		}
	}
	for _, index := range unique {
		quotaMutationLocks[index].Lock()
	}
	return func() {
		for index := len(unique) - 1; index >= 0; index-- {
			quotaMutationLocks[unique[index]].Unlock()
		}
	}
}

func init() {
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateStores = append(batchUpdateStores, make(map[int]int))
		batchUpdateLocks = append(batchUpdateLocks, sync.Mutex{})
	}
}

func InitBatchUpdater() {
	modelBackground.Go(func(ctx context.Context) {
		interval := time.Duration(common.BatchUpdateInterval) * time.Second
		if interval <= 0 {
			interval = time.Second
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := FlushBatchUpdates(ctx); err != nil {
					common.SysError("batch update failed: " + err.Error())
				}
			}
		}
	})
}

func addNewRecord(type_ int, id int, value int) {
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	if _, ok := batchUpdateStores[type_][id]; !ok {
		batchUpdateStores[type_][id] = value
	} else {
		batchUpdateStores[type_][id] += value
	}
}

type quotaFlushBatch struct {
	id     string
	stores []map[int]int
}

var pendingQuotaFlush *quotaFlushBatch

func batchUpdate() {
	if err := FlushBatchUpdates(context.Background()); err != nil {
		common.SysError("batch update failed: " + err.Error())
	}
}
func FlushBatchUpdates(ctx context.Context) error {
	quotaBatchMutationLock.Lock()
	defer quotaBatchMutationLock.Unlock()
	if err := flushPendingQuotaBatchLocked(ctx); err != nil {
		return err
	}
	stores := make([]map[int]int, BatchUpdateTypeCount)
	hasData := false
	for i := range stores {
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		hasData = hasData || len(stores[i]) != 0
		batchUpdateLocks[i].Unlock()
	}
	if !hasData {
		return nil
	}
	pendingQuotaFlush = &quotaFlushBatch{id: common.GetUUID(), stores: stores}
	return flushPendingQuotaBatchLocked(ctx)
}

// Caller holds quotaBatchMutationLock. Recovery resolves any frozen batch
// before extracting newer deltas, even after an ambiguous commit failure.
func flushPendingQuotaBatchLocked(ctx context.Context) error {
	if pendingQuotaFlush == nil {
		return nil
	}
	batch := pendingQuotaFlush
	err := ApplyFlushOperation(ctx, batch.id, func(tx *gorm.DB) error {
		for kind, store := range batch.stores {
			for id, delta := range store {
				if delta == 0 {
					continue
				}
				var result *gorm.DB
				switch kind {
				case BatchUpdateTypeUserQuota:
					result = tx.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", delta))
				case BatchUpdateTypeUsedQuota:
					result = tx.Model(&User{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", delta))
				case BatchUpdateTypeRequestCount:
					result = tx.Model(&User{}).Where("id = ?", id).Update("request_count", gorm.Expr("request_count + ?", delta))
				case BatchUpdateTypeTokenQuota:
					result = tx.Unscoped().Model(&Token{}).Where("id = ?", id).Updates(map[string]any{"remain_quota": gorm.Expr("remain_quota + ?", delta), "used_quota": gorm.Expr("used_quota - ?", delta), "accessed_time": common.GetTimestamp()})
				case BatchUpdateTypeChannelUsedQuota:
					result = tx.Model(&Channel{}).Where("id = ?", id).Update("used_quota", gorm.Expr("used_quota + ?", delta))
				}
				if result.Error != nil {
					return result.Error
				}
				if result.RowsAffected != 1 {
					return fmt.Errorf("batch resource missing: kind=%d id=%d", kind, id)
				}
			}
		}
		return nil
	})
	if err == nil {
		pendingQuotaFlush = nil
	}
	return err
}

func RecordExist(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func shouldUpdateRedis(fromDB bool, err error) bool {
	return common.RedisEnabled && fromDB && err == nil
}
