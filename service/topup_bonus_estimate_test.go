package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateTopUpBonusReplaysEverySource(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.Redemption{}, &model.AdminQuotaOperation{}))
	cutoff := topUpBonusTestCutoff
	// User 7 spends 100 on each of the 30 replayed days.
	for daysBack := 1; daysBack <= 30; daysBack++ {
		seedTopUpBonusRows(t, db, model.TopUpBonusUsageHourly{UserID: 7, HourUTC: cutoff - int64(daysBack)*86400, Quota: 100})
	}
	late := cutoff - 86400 + 3600
	order := func(tradeNo string, userID int, created int64, quota, bonus int, provider, status string) {
		require.NoError(t, db.Create(&model.TopUp{UserId: userID, TradeNo: tradeNo, CreateTime: created, ExpectedQuota: quota, BonusQuota: bonus, PaymentProvider: provider, Status: status}).Error)
	}
	// One order early in the window (no month history yet) and the rest late.
	order("early", 7, cutoff-29*86400+3600, 1000, 0, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	order("late", 7, late, 1000, 0, model.PaymentProviderGmpay, common.TopUpStatusSuccess)
	order("excluded", 1, late, 1000, 0, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	order("stripe-bonus", 7, late, 1050, 50, model.PaymentProviderStripe, common.TopUpStatusSuccess)
	order("unrated", 8, late, 0, 0, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	order("balance", 7, late, 1000, 0, model.PaymentProviderBalance, common.TopUpStatusSuccess)
	order("pending", 7, late, 1000, 0, model.PaymentProviderEpay, common.TopUpStatusPending)
	order("today", 7, cutoff+3600, 1000, 0, model.PaymentProviderEpay, common.TopUpStatusSuccess)
	// Redemptions count for the redeemer, including codes deleted after use.
	require.NoError(t, db.Create(&[]model.Redemption{
		{Key: "used", UserId: 2, UsedUserId: 7, Status: common.RedemptionCodeStatusUsed, Quota: 1000, RedeemedTime: late},
		{Key: "deleted", UserId: 2, UsedUserId: 7, Status: common.RedemptionCodeStatusUsed, Quota: 99, RedeemedTime: late},
		{Key: "unused", UserId: 2, Status: common.RedemptionCodeStatusEnabled, Quota: 1000},
	}).Error)
	require.NoError(t, db.Where("key = ?", "deleted").Delete(&model.Redemption{}).Error)
	// Additions count with their principal; an empty mode is a legacy addition.
	require.NoError(t, db.Create(&[]model.AdminQuotaOperation{
		{OperatorID: 9, OperationKey: "a", UserID: 7, Mode: "add", Value: 1000, CreatedAt: late},
		{OperatorID: 9, OperationKey: "b", UserID: 7, Mode: "", Value: 1000, CreatedAt: late},
		{OperatorID: 9, OperationKey: "c", UserID: 7, Mode: "subtract", Value: 1000, CreatedAt: late},
		{OperatorID: 0, OperationKey: "d", UserID: 7, Mode: "add", Value: 1000, CreatedAt: late},
	}).Error)

	config := model.DefaultTopUpBonusConfig()
	config.FixedRate = "0.01"
	config.Personal.Day = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOff, Tiers: topUpBonusTiers()}
	config.Personal.Week = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOff, Tiers: topUpBonusTiers()}
	config.Personal.Month = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeShadow, Tiers: topUpBonusTiers("1000:0.02")}
	data, err := common.Marshal(config)
	require.NoError(t, err)

	estimate, err := EstimateTopUpBonus(data, topUpBonusTestNow)
	require.NoError(t, err)
	assert.Equal(t, "2026-09-30", estimate.FromDate)
	assert.Equal(t, "2026-10-29", estimate.ToDate)
	// early: 1%; late credits of user 7: month window 2900 ≥ 1000 → 1% + 2%
	// (shadow replayed as on); excluded: fixed 1% only.
	assert.Equal(t, []TopUpBonusEstimateSource{
		{Source: model.TopUpBonusSourceTopUp, Records: 4, PrincipalQuota: 4000, BonusQuota: 10 + 30 + 10 + 30},
		{Source: model.TopUpBonusSourceRedemption, Records: 2, PrincipalQuota: 1099, BonusQuota: 30 + 2},
		{Source: model.TopUpBonusSourceAdminQuota, Records: 2, PrincipalQuota: 2000, BonusQuota: 60},
	}, estimate.Sources, "each credit's bonus is floored on its own: 99 × 3% gives 2")
	assert.Equal(t, 8, estimate.Records)
	assert.Equal(t, 1, estimate.UnratedRecords)
	assert.Equal(t, int64(7099), estimate.PrincipalQuota)
	assert.Equal(t, int64(172), estimate.BonusQuota)
	assert.Equal(t, "0.0242", estimate.BonusShare)
	assert.Equal(t, "0.0000", estimate.CappedPrincipalShare)
	require.NotNil(t, estimate.EarliestHour)
	assert.False(t, estimate.HistoryCoversStart, "60 days of history are needed")
	assert.Nil(t, currentTopUpBonusDay.Load(), "an estimate does not touch the request cache")

	config.Timezone = "Asia/Kolkata"
	data, err = common.Marshal(config)
	require.NoError(t, err)
	_, err = EstimateTopUpBonus(data, topUpBonusTestNow)
	assert.ErrorIs(t, err, model.ErrTopUpBonusInvalidConfig)
}

func TestEstimateTopUpBonusBucketsOrdersByLocalMidnights(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.TopUp{}, &model.Redemption{}, &model.AdminQuotaOperation{}))
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// 2026-11-01 has 25 hours, so a fixed 24-hour bucket puts the last
	// order of 2026-11-09 one day too far and drops it.
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, location)
	lastDay := time.Date(2026, 11, 9, 0, 0, 0, 0, location).Unix()
	require.NoError(t, db.Create(&model.TopUp{UserId: 7, TradeNo: "late", CreateTime: time.Date(2026, 11, 9, 23, 30, 0, 0, location).Unix(),
		ExpectedQuota: 1000, PaymentProvider: model.PaymentProviderEpay, Status: common.TopUpStatusSuccess}).Error)
	// 600 of day usage on 2026-11-08 reaches the day tier only for orders of 2026-11-09.
	seedTopUpBonusRows(t, db, model.TopUpBonusUsageHourly{UserID: 7, HourUTC: lastDay - 3600, Quota: 600})

	config := model.DefaultTopUpBonusConfig()
	config.Timezone = "America/New_York"
	config.Personal.Day = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("500:0.03")}
	config.Personal.Week = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOff, Tiers: topUpBonusTiers()}
	config.Personal.Month = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOff, Tiers: topUpBonusTiers()}
	data, err := common.Marshal(config)
	require.NoError(t, err)

	estimate, err := EstimateTopUpBonus(data, now)
	require.NoError(t, err)
	assert.Equal(t, "2026-11-09", estimate.ToDate)
	assert.Equal(t, 1, estimate.Records, "the 23:30 order belongs to the last replayed day")
	assert.Equal(t, int64(30), estimate.BonusQuota, "rated with the day window that ends at its own midnight")
}
