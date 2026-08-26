package model

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
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
	gopool.Go(func() {
		for {
			time.Sleep(time.Duration(common.BatchUpdateInterval) * time.Second)
			batchUpdate()
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

func batchUpdate() {
	quotaBatchMutationLock.Lock()
	defer quotaBatchMutationLock.Unlock()

	// check if there's any data to update
	hasData := false
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		if len(batchUpdateStores[i]) > 0 {
			hasData = true
			batchUpdateLocks[i].Unlock()
			break
		}
		batchUpdateLocks[i].Unlock()
	}

	if !hasData {
		return
	}

	common.SysLog("batch update started")
	stores := make([]map[int]int, BatchUpdateTypeCount)
	for i := 0; i < BatchUpdateTypeCount; i++ {
		batchUpdateLocks[i].Lock()
		stores[i] = batchUpdateStores[i]
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}

	for i, store := range stores {
		if i == BatchUpdateTypeUserQuota || i == BatchUpdateTypeUsedQuota || i == BatchUpdateTypeRequestCount {
			continue
		}
		for key, value := range store {
			switch i {
			case BatchUpdateTypeTokenQuota:
				err := increaseTokenQuota(key, value)
				if err != nil {
					common.SysLog("failed to batch update token quota: " + err.Error())
				}
			case BatchUpdateTypeChannelUsedQuota:
				updateChannelUsedQuota(key, value)
			}
		}
	}

	userQuotaStore := stores[BatchUpdateTypeUserQuota]
	usedQuotaStore := stores[BatchUpdateTypeUsedQuota]
	requestCountStore := stores[BatchUpdateTypeRequestCount]

	userIDs := make(map[int]struct{}, len(userQuotaStore)+len(usedQuotaStore)+len(requestCountStore))
	for key := range userQuotaStore {
		userIDs[key] = struct{}{}
	}
	for key := range usedQuotaStore {
		userIDs[key] = struct{}{}
	}
	for key := range requestCountStore {
		userIDs[key] = struct{}{}
	}
	for key := range userIDs {
		updateUserQuotaUsedQuotaAndRequestCount(key, userQuotaStore[key], usedQuotaStore[key], requestCountStore[key])
	}
	common.SysLog("batch update finished")
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
