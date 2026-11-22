package model

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// Reservations must not race a detached batch or another cache debit that has
// not yet entered the batch. This is the single-node quota persistence boundary.
var quotaReserveBatchLocks [2][256]sync.Mutex
var quotaCacheNeedsReload [2]sync.Map

func lockQuotaReservation(kind, id int) func() {
	lock := &quotaReserveBatchLocks[kind][uint(id)%256]
	lock.Lock()
	return lock.Unlock
}

// The caller holds the reservation lock for this balance. Database work must
// not hold the shared batch lock; a failed detached delta is merged back.
func flushPendingQuota(kind, id int) error {
	batchUpdateLocks[kind].Lock()
	delta := batchUpdateStores[kind][id]
	delete(batchUpdateStores[kind], id)
	batchUpdateLocks[kind].Unlock()
	if delta == 0 {
		return nil
	}
	result := writeQuotaBatch(DB, kind, id, delta)
	if result.Error == nil && result.RowsAffected == 0 {
		// Soft deletion does not cancel charges or refunds already accepted.
		result = writeQuotaBatch(DB.Unscoped(), kind, id, delta)
	}
	if result.Error != nil {
		addNewRecord(kind, id, delta)
		return result.Error
	}
	if result.RowsAffected == 0 {
		common.SysError(common.LogText("discarding quota batch for deleted record: type=%d id=%d delta=%d", kind, id, delta))
	}
	return nil
}

func writeQuotaBatch(db *gorm.DB, kind, id, delta int) *gorm.DB {
	if kind == BatchUpdateTypeUserQuota {
		return db.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", delta))
	}
	return db.Model(&Token{}).Where("id = ?", id).Updates(map[string]any{
		"remain_quota":  gorm.Expr("remain_quota + ?", delta),
		"used_quota":    gorm.Expr("used_quota - ?", delta),
		"accessed_time": common.GetTimestamp(),
	})
}

func flushQuotaBatches() {
	for _, kind := range []int{BatchUpdateTypeUserQuota, BatchUpdateTypeTokenQuota} {
		batchUpdateLocks[kind].Lock()
		ids := make([]int, 0, len(batchUpdateStores[kind]))
		for id := range batchUpdateStores[kind] {
			ids = append(ids, id)
		}
		batchUpdateLocks[kind].Unlock()
		for _, id := range ids {
			unlock := lockQuotaReservation(kind, id)
			if err := flushPendingQuota(kind, id); err != nil {
				common.SysError(common.LogText("failed to flush quota batch: type=%d id=%d error=%v", kind, id, err))
			}
			unlock()
		}
	}
}

func prepareQuotaCache(kind, id int, key string) error {
	if _, dirty := quotaCacheNeedsReload[kind].Load(id); !dirty {
		return nil
	}
	if err := flushPendingQuota(kind, id); err != nil {
		return err
	}
	return invalidateQuotaCache(kind, id, key)
}

func invalidateQuotaCache(kind, id int, key string) error {
	var err error
	if kind == BatchUpdateTypeUserQuota {
		err = invalidateUserCache(id)
	} else {
		err = invalidateTokenCacheForMutation(key)
	}
	if err == nil {
		quotaCacheNeedsReload[kind].Delete(id)
	}
	return err
}

func quotaCacheDirty(kind, id int) bool {
	_, dirty := quotaCacheNeedsReload[kind].Load(id)
	return dirty
}

// Cache mutation and queue publication share the same boundary as hydration
// and reservation. A missing cache is safe: the queued delta is flushed before
// hydration. An uncertain Redis result must invalidate before any future debit.
func queueQuotaDelta(kind, id int, key string, delta int) error {
	unlock := lockQuotaReservation(kind, id)
	defer unlock()
	updateQuotaCacheLocked(kind, id, key, delta, false)
	addNewRecord(kind, id, delta)
	return nil
}
