package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateUsageDiscountReplaysCompleteDays(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	cutoff := usageDiscountTestCutoff
	// User 7 spends 100 on each of the 30 replayed days; user 8's refund day
	// and excluded user 1 add no revenue.
	for daysBack := 1; daysBack <= 30; daysBack++ {
		seedUsageDiscountRows(t, db, model.UsageDiscountHourly{UserID: 7, HourUTC: cutoff - int64(daysBack)*86400, Quota: 100})
	}
	seedUsageDiscountRows(t, db,
		model.UsageDiscountHourly{UserID: 8, HourUTC: cutoff - 2*86400, Quota: -50},
		model.UsageDiscountHourly{UserID: 1, HourUTC: cutoff - 86400, Quota: 10_000},
		model.UsageDiscountHourly{UserID: 7, HourUTC: cutoff, Quota: 777}, // the current day is not replayed
	)
	config := model.DefaultUsageDiscountConfig()
	config.FixedRate = "0.01"
	config.Personal.Day = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeOff, Tiers: usageDiscountTiers()}
	config.Personal.Week = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeOff, Tiers: usageDiscountTiers()}
	config.Personal.Month = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeShadow, Tiers: usageDiscountTiers("1000:0.02")}
	data, err := common.Marshal(config)
	require.NoError(t, err)

	estimate, err := EstimateUsageDiscount(data, usageDiscountTestNow)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-30", estimate.FromDate)
	assert.Equal(t, "2026-10-29", estimate.ToDate)
	assert.Equal(t, int64(3000), estimate.RevenueQuota)
	// The shadow month table is replayed as on: the 20 days whose prior 30 days
	// hold at least 1000 get 1% + 2%, the 10 earliest only 1%: 60 + 10.
	assert.Equal(t, int64(70), estimate.DiscountQuota)
	assert.Equal(t, "0.0233", estimate.DiscountShare)
	assert.Equal(t, "0.0000", estimate.CappedRevenueShare)
	require.NotNil(t, estimate.EarliestHour)
	assert.Equal(t, cutoff-30*86400, *estimate.EarliestHour)
	assert.False(t, estimate.HistoryComplete, "60 days of history are needed")
	assert.Nil(t, currentUsageDiscountDay.Load(), "an estimate does not touch the request cache")

	config.Timezone = "Asia/Kolkata"
	data, err = common.Marshal(config)
	require.NoError(t, err)
	_, err = EstimateUsageDiscount(data, usageDiscountTestNow)
	assert.ErrorIs(t, err, model.ErrUsageDiscountInvalidConfig)
}
