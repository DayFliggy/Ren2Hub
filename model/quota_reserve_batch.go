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

// The caller holds the reservation lock for this balance. Keep the batch entry until its write
// succeeds, including when the periodic updater encounters a database failure.
func flushPendingQuota(kind, id int) error {
	batchUpdateLocks[kind].Lock()
	defer batchUpdateLocks[kind].Unlock()
	delta, exists := batchUpdateStores[kind][id]
	if !exists || delta == 0 {
		delete(batchUpdateStores[kind], id)
		return nil
	}
	var result *gorm.DB
	if kind == BatchUpdateTypeUserQuota {
		result = DB.Model(&User{}).Where("id = ?", id).Update("quota", gorm.Expr("quota + ?", delta))
	} else {
		result = DB.Model(&Token{}).Where("id = ?", id).Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota + ?", delta),
			"used_quota":    gorm.Expr("used_quota - ?", delta),
			"accessed_time": common.GetTimestamp(),
		})
	}
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	delete(batchUpdateStores[kind], id)
	return nil
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
