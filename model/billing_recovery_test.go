package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMarkBillingRecoveryRefundedRequiresCompletedComponents(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))

	previousDB := DB
	previousMainDBType := common.MainDatabaseType()
	previousLogDBType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
	})

	const requestID = "billing-recovery-completion-test"
	_, err = EnsureBillingRecovery(BillingRecoveryInput{
		RequestID: requestID,
		UserID:    1,
		Source:    "wallet",
	})
	require.NoError(t, err)
	require.NoError(t, UpdateBillingRecoveryAmounts(requestID, 0, 10, 4, 0))
	require.NoError(t, MarkBillingRecoveryRefundPending(requestID, assert.AnError))

	err = MarkBillingRecoveryRefunded(requestID, nil)
	assert.ErrorIs(t, err, ErrBillingRecoveryStateConflict)

	var recovery BillingRecovery
	require.NoError(t, DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusRefundPending, recovery.Status)
	assert.NotEmpty(t, recovery.LastError)

	require.NoError(t, DB.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
		"funding_refund": BillingRecoveryStateCompleted,
		"extra_refund":   BillingRecoveryStateCompleted,
	}).Error)
	require.NoError(t, MarkBillingRecoveryRefunded(requestID, nil))
	require.NoError(t, DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusRefunded, recovery.Status)
}

func TestBillingRecoveryPreConsumePersistsAndHydratesRecoverableFacts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))

	previousDB := DB
	previousMainDBType := common.MainDatabaseType()
	previousLogDBType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
	})

	const walletRequestID = "billing-recovery-wallet-preconsume-test"
	require.NoError(t, DB.Create(&User{Id: 1, Username: "wallet-user", Quota: 100}).Error)
	require.NoError(t, DB.Create(&Token{Id: 1, UserId: 1, Key: "wallet-token", RemainQuota: 100}).Error)
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: walletRequestID, UserID: 1, TokenID: 1, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, PreConsumeWalletBillingRecovery(walletRequestID, 40, false, false))

	var walletRecovery BillingRecovery
	var user User
	var token Token
	require.NoError(t, DB.Where("request_id = ?", walletRequestID).First(&walletRecovery).Error)
	require.NoError(t, DB.Select("quota").Where("id = ?", 1).First(&user).Error)
	require.NoError(t, DB.Select("remain_quota", "used_quota").Where("id = ?", 1).First(&token).Error)
	assert.Equal(t, BillingRecoveryPreConsumeCommitted, walletRecovery.PreConsumeState)
	assert.Equal(t, int64(40), walletRecovery.FundingAmount)
	assert.Equal(t, int64(40), walletRecovery.TokenAmount)
	assert.Equal(t, 60, user.Quota)
	assert.Equal(t, 60, token.RemainQuota)
	assert.Equal(t, 40, token.UsedQuota)

	const subscriptionRequestID = "billing-recovery-subscription-hydrate-test"
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: subscriptionRequestID, UserID: 2, TokenID: 2, Source: "subscription"})
	require.NoError(t, err)
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId: subscriptionRequestID, UserId: 2, UserSubscriptionId: 9, PreConsumed: 25, Status: "consumed",
	}).Error)
	require.NoError(t, MarkBillingRecoveryRefundPending(subscriptionRequestID, assert.AnError))

	var subscriptionRecovery BillingRecovery
	require.NoError(t, DB.Where("request_id = ?", subscriptionRequestID).First(&subscriptionRecovery).Error)
	assert.Equal(t, BillingRecoveryPreConsumeCommitted, subscriptionRecovery.PreConsumeState)
	assert.Equal(t, 9, subscriptionRecovery.SubscriptionID)
	assert.Equal(t, int64(25), subscriptionRecovery.FundingAmount)
	assert.Equal(t, int64(25), subscriptionRecovery.TokenAmount)
	assert.Equal(t, BillingRecoveryStatusRefundPending, subscriptionRecovery.Status)
}

func TestListStaleBillingRecoveriesIncludesPreparedSubscriptionPreConsume(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&UserSubscription{}, &SubscriptionPreConsumeRecord{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))

	previousDB := DB
	previousMainDBType := common.MainDatabaseType()
	previousLogDBType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
	})

	const requestID = "billing-recovery-prepared-subscription-stale-test"
	const subscriptionID = 21
	require.NoError(t, DB.Create(&UserSubscription{
		Id: subscriptionID, UserId: 3, AmountTotal: 100, AmountUsed: 30, Status: "active",
	}).Error)
	_, err = EnsureBillingRecovery(BillingRecoveryInput{
		RequestID: requestID,
		UserID:    3,
		Source:    "subscription",
	})
	require.NoError(t, err)
	require.NoError(t, DB.Create(&SubscriptionPreConsumeRecord{
		RequestId: requestID, UserId: 3, UserSubscriptionId: subscriptionID, PreConsumed: 30, Status: "consumed",
	}).Error)
	require.NoError(t, DB.Model(&BillingRecovery{}).Where("request_id = ?", requestID).
		Update("updated_at", common.GetTimestamp()-3600).Error)

	rows, err := ListStaleBillingRecoveries(common.GetTimestamp()-30, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, requestID, rows[0].RequestID)
	assert.Equal(t, BillingRecoveryPreConsumePrepared, rows[0].PreConsumeState)

	require.NoError(t, MarkBillingRecoveryRefundPending(requestID, nil))
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationRefund))
	require.NoError(t, MarkBillingRecoveryRefunded(requestID, nil))

	var subscription UserSubscription
	require.NoError(t, DB.Where("id = ?", subscriptionID).First(&subscription).Error)
	assert.Equal(t, int64(0), subscription.AmountUsed)
	var record SubscriptionPreConsumeRecord
	require.NoError(t, DB.Where("request_id = ?", requestID).First(&record).Error)
	assert.Equal(t, "refunded", record.Status)
}

func TestSubscriptionBillingRecoveryMissingPreConsumeRecordRemainsPending(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SubscriptionPreConsumeRecord{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))

	previousDB := DB
	previousMainDBType := common.MainDatabaseType()
	previousLogDBType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
	})

	const requestID = "billing-recovery-missing-preconsume-test"
	_, err = EnsureBillingRecovery(BillingRecoveryInput{
		RequestID: requestID,
		UserID:    4,
		Source:    "subscription",
	})
	require.NoError(t, err)
	// Simulate a hydrated recovery row whose idempotency record was lost or
	// never committed. The funding amount cannot be refunded safely without
	// that record, so the recovery must remain retryable.
	require.NoError(t, UpdateBillingRecoveryAmounts(requestID, 22, 30, 0, 0))
	require.NoError(t, MarkBillingRecoveryRefundPending(requestID, nil))

	err = ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationRefund)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	var recovery BillingRecovery
	require.NoError(t, DB.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusRefundPending, recovery.Status)
	assert.Equal(t, BillingRecoveryStatePending, recovery.FundingRefund)
	assert.ErrorIs(t, MarkBillingRecoveryRefunded(requestID, nil), ErrBillingRecoveryStateConflict)
}
