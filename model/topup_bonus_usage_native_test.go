package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The native used_quota counters keep the upstream behavior: they move
// used_quota and request_count, directly or through the batch queue, and
// write nothing to the top-up bonus window. Only the synchronous settlement
// paths in service record the window.
func TestNativeUsageCountersLeaveTheTopUpBonusWindowAlone(t *testing.T) {
	for _, batch := range []bool{false, true} {
		name := "direct"
		if batch {
			name = "batch"
		}
		t.Run(name, func(t *testing.T) {
			setupUserUpdateTestState(t)
			resetBatchUpdateTestState(t)
			// Migrated, so a missing write is not hidden by a failing one.
			require.NoError(t, DB.AutoMigrate(&TopUpBonusUsageHourly{}))
			require.NoError(t, DB.Where("1 = 1").Delete(&TopUpBonusUsageHourly{}).Error)
			t.Cleanup(func() { require.NoError(t, DB.Where("1 = 1").Delete(&TopUpBonusUsageHourly{}).Error) })
			user := User{Id: 31, Username: "bonus-native-counters", Password: "password", Status: common.UserStatusEnabled, UsedQuota: 1000, RequestCount: 3}
			require.NoError(t, DB.Create(&user).Error)
			common.BatchUpdateEnabled = batch

			UpdateUserUsedQuotaAndRequestCount(user.Id, 300)
			UpdateUserUsedQuota(user.Id, -50)
			if batch {
				batchUpdate()
			}

			var got User
			require.NoError(t, DB.Select("used_quota", "request_count").First(&got, user.Id).Error)
			assert.Equal(t, 1250, got.UsedQuota)
			assert.Equal(t, 4, got.RequestCount)
			var rows int64
			require.NoError(t, DB.Model(&TopUpBonusUsageHourly{}).Count(&rows).Error)
			assert.Zero(t, rows)
		})
	}
}
