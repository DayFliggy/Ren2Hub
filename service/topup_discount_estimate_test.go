package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateTopUpDiscountReplaysTopUps(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	cutoff := topUpDiscountTestCutoff
	// User 7 spends 100 on each of the 30 replayed days.
	for daysBack := 1; daysBack <= 30; daysBack++ {
		seedTopUpDiscountRows(t, db, model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: cutoff - int64(daysBack)*86400, Quota: 100})
	}
	order := func(tradeNo string, userID int, created int64, quota int, provider, status string) {
		require.NoError(t, db.Create(&model.TopUp{UserId: userID, TradeNo: tradeNo, CreateTime: created, ExpectedQuota: quota, PaymentProvider: provider, Status: status}).Error)
	}
	// One order early in the window (no month history yet) and one late.
	order("early", 7, cutoff-29*86400+3600, 1000, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	order("late", 7, cutoff-86400+3600, 1000, model.PaymentProviderGmpay, common.TopUpStatusSuccess)
	order("excluded", 1, cutoff-86400+3600, 1000, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	order("unrated", 8, cutoff-86400+3600, 0, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	order("stripe", 7, cutoff-86400+3600, 1000, model.PaymentProviderStripe, common.TopUpStatusSuccess)
	order("pending", 7, cutoff-86400+3600, 1000, model.PaymentProviderEpay, common.TopUpStatusPending)
	order("today", 7, cutoff+3600, 1000, model.PaymentProviderEpay, common.TopUpStatusSuccess)

	config := model.DefaultTopUpDiscountConfig()
	config.FixedRate = "0.01"
	config.Personal.Day = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOff, Tiers: topUpDiscountTiers()}
	config.Personal.Week = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOff, Tiers: topUpDiscountTiers()}
	config.Personal.Month = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeShadow, Tiers: topUpDiscountTiers("1000:0.02")}
	data, err := common.Marshal(config)
	require.NoError(t, err)

	estimate, err := EstimateTopUpDiscount(data, topUpDiscountTestNow)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-30", estimate.FromDate)
	assert.Equal(t, "2026-10-29", estimate.ToDate)
	assert.Equal(t, 3, estimate.Orders, "early, late and excluded; Stripe, pending and today are out")
	assert.Equal(t, 1, estimate.UnratedOrders)
	assert.Equal(t, int64(3000), estimate.CreditedQuota)
	// early: 1%; late: month window 2900 ≥ 1000 → 1% + 2% (shadow replayed
	// as on); excluded: fixed 1% only. 10 + 30 + 10.
	assert.Equal(t, int64(50), estimate.DiscountQuota)
	assert.Equal(t, "0.0167", estimate.DiscountShare)
	assert.Equal(t, "0.0000", estimate.CappedQuotaShare)
	require.NotNil(t, estimate.EarliestHour)
	assert.False(t, estimate.HistoryCoversStart, "60 days of history are needed")
	assert.Nil(t, currentTopUpDiscountDay.Load(), "an estimate does not touch the request cache")

	config.Timezone = "Asia/Kolkata"
	data, err = common.Marshal(config)
	require.NoError(t, err)
	_, err = EstimateTopUpDiscount(data, topUpDiscountTestNow)
	assert.ErrorIs(t, err, model.ErrTopUpDiscountInvalidConfig)
}

func TestEstimateTopUpDiscountBucketsOrdersByLocalMidnights(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// 2026-11-01 has 25 hours, so a fixed 24-hour bucket puts the last
	// order of 2026-11-09 one day too far and drops it.
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, location)
	lastDay := time.Date(2026, 11, 9, 0, 0, 0, 0, location).Unix()
	require.NoError(t, db.Create(&model.TopUp{UserId: 7, TradeNo: "late", CreateTime: time.Date(2026, 11, 9, 23, 30, 0, 0, location).Unix(),
		ExpectedQuota: 1000, PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusSuccess}).Error)
	// 600 of day usage on 2026-11-08 reaches the day tier only for orders of 2026-11-09.
	seedTopUpDiscountRows(t, db, model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: lastDay - 3600, Quota: 600})

	config := model.DefaultTopUpDiscountConfig()
	config.Timezone = "America/New_York"
	config.Personal.Day = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("500:0.03")}
	config.Personal.Week = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOff, Tiers: topUpDiscountTiers()}
	config.Personal.Month = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOff, Tiers: topUpDiscountTiers()}
	data, err := common.Marshal(config)
	require.NoError(t, err)

	estimate, err := EstimateTopUpDiscount(data, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-09", estimate.ToDate)
	assert.Equal(t, 1, estimate.Orders, "the 23:30 order belongs to the last replayed day")
	assert.Equal(t, int64(30), estimate.DiscountQuota, "rated with the day window that ends at its own midnight")
}
