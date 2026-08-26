package model

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupBillingRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&User{}, &Token{}, &UserSubscription{}, &SubscriptionPreConsumeRecord{},
		&BillingRecovery{}, &BillingRecoveryAdjustment{},
	))

	previousDB := DB
	previousMainDBType := common.MainDatabaseType()
	previousLogDBType := common.LogDatabaseType()
	DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB = previousDB
		common.SetDatabaseTypes(previousMainDBType, previousLogDBType)
	})
	return db
}

func TestBillingRecoveryRejectsIdentityConflictAndRebindsOnlyAbortedAttempt(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	input := BillingRecoveryInput{RequestID: "recovery-rebind", UserID: 1, TokenID: 2, Source: "wallet", TokenRequired: true}
	_, err := EnsureBillingRecovery(input)
	require.NoError(t, err)

	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: input.RequestID, UserID: 9, TokenID: 2, Source: "wallet", TokenRequired: true})
	assert.Error(t, err)
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: input.RequestID, UserID: 1, TokenID: 2, Source: "subscription", TokenRequired: true})
	assert.Error(t, err)

	require.NoError(t, AbortPreparedBillingRecovery(input.RequestID, assert.AnError))
	rebound, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: input.RequestID, UserID: 1, TokenID: 2, Source: "subscription", TokenRequired: true})
	require.NoError(t, err)
	assert.Equal(t, "subscription", rebound.Source)
	assert.Equal(t, BillingRecoveryStatusActive, rebound.Status)

	require.NoError(t, UpdateBillingRecoveryAmounts(input.RequestID, 0, 10, 0, 10))
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: input.RequestID, UserID: 1, TokenID: 2, Source: "wallet", TokenRequired: true})
	assert.Error(t, err)
	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", input.RequestID).First(&recovery).Error)
	assert.Equal(t, "subscription", recovery.Source)
}

func TestMarkBillingRecoveryRefundedRequiresCompletedComponents(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	const requestID = "billing-recovery-completion"
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: requestID, UserID: 1, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, UpdateBillingRecoveryAmounts(requestID, 0, 10, 4, 0))
	require.NoError(t, MarkBillingRecoveryRefundPending(requestID, assert.AnError))

	err = MarkBillingRecoveryRefunded(requestID, nil)
	assert.ErrorIs(t, err, ErrBillingRecoveryStateConflict)

	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
		"funding_refund": BillingRecoveryStateCompleted,
		"extra_refund":   BillingRecoveryStateCompleted,
	}).Error)
	require.NoError(t, MarkBillingRecoveryRefunded(requestID, nil))

	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusRefunded, recovery.Status)
}

func TestBillingRecoveryWalletPreConsumePersistsActualTokenParticipation(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.Create(&User{Id: 1, Username: "recovery-user", Quota: 100}).Error)
	require.NoError(t, db.Create(&Token{Id: 1, UserId: 1, Key: "recovery-token", RemainQuota: 100}).Error)

	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: "wallet-token", UserID: 1, TokenID: 1, Source: "wallet", TokenRequired: true})
	require.NoError(t, err)
	require.NoError(t, PreConsumeWalletBillingRecovery("wallet-token", 40, false))
	require.NoError(t, PreConsumeWalletBillingRecovery("wallet-token", 40, false))

	var charged BillingRecovery
	var user User
	var token Token
	require.NoError(t, db.Where("request_id = ?", "wallet-token").First(&charged).Error)
	require.NoError(t, db.Select("quota").Where("id = ?", 1).First(&user).Error)
	require.NoError(t, db.Select("remain_quota", "used_quota").Where("id = ?", 1).First(&token).Error)
	assert.Equal(t, int64(40), charged.FundingAmount)
	assert.Equal(t, int64(40), charged.TokenAmount)
	assert.Equal(t, 60, user.Quota)
	assert.Equal(t, 60, token.RemainQuota)
	assert.Equal(t, 40, token.UsedQuota)

	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: "wallet-playground", UserID: 1, TokenID: 1, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, PreConsumeWalletBillingRecovery("wallet-playground", 10, false))
	var playground BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "wallet-playground").First(&playground).Error)
	require.NoError(t, db.Select("remain_quota", "used_quota").Where("id = ?", 1).First(&token).Error)
	assert.Zero(t, playground.TokenAmount)
	assert.Equal(t, 60, token.RemainQuota)
	assert.Equal(t, 40, token.UsedQuota)
}

func TestBillingRecoveryWalletPreConsumeFencesQuotaCachesBeforeMutation(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	server := useUserCacheMiniRedis(t)
	user := User{Id: 1, Username: "recovery-cache-user", Quota: 100, AuthVersion: 1}
	token := Token{Id: 1, UserId: 1, Key: "recovery-cache-token", RemainQuota: 100}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	require.NoError(t, populateUserCache(user))
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)

	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: "wallet-cache-fence", UserID: user.Id, TokenID: token.Id, Source: "wallet", TokenRequired: true})
	require.NoError(t, err)
	require.NoError(t, PreConsumeWalletBillingRecovery("wallet-cache-fence", 40, false))

	assert.True(t, server.Exists(getUserQuotaMutationFenceKey(user.Id)))
	assert.True(t, server.Exists(getTokenCacheFenceKey(token.Key)))
	assert.False(t, server.Exists(getUserCacheKey(user.Id)))
	assert.False(t, server.Exists(getTokenCacheKey(token.Key)))
	reserved, err := TryReserveUserQuota(user.Id, 70)
	require.NoError(t, err)
	assert.False(t, reserved, "a fenced cache must fall back to the current database balance")
}

func TestBillingRecoveryFlushesPendingQuotaBeforeCacheFence(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	resetBatchUpdateTestState(t)
	server := useUserCacheMiniRedis(t)
	common.BatchUpdateEnabled = true

	user := User{Id: 1, Username: "recovery-batch-user", Quota: 100, AuthVersion: 1}
	token := Token{Id: 1, UserId: user.Id, Key: "recovery-batch-token", RemainQuota: 100}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&token).Error)
	_, err := EnsureBillingRecovery(BillingRecoveryInput{
		RequestID: "recovery-batch-fence", UserID: user.Id, TokenID: token.Id, Source: "wallet", TokenRequired: true,
	})
	require.NoError(t, err)
	require.NoError(t, PreConsumeWalletBillingRecovery("recovery-batch-fence", 40, false))

	// The initial durable pre-consume fenced both caches. After the fence
	// expires, these reservations remain only in Redis plus batch queues.
	server.FastForward(time.Duration(quotaMutationFenceSeconds+1) * time.Second)
	reserved, err := TryReserveUserQuota(user.Id, 50)
	require.NoError(t, err)
	require.True(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 50, false)
	require.NoError(t, err)
	require.True(t, reserved)
	assert.Equal(t, 60, getUserQuotaFromDB(t, user.Id), "queued user reservation is not flushed yet")
	assert.Equal(t, 60, getTokenFromDB(t, token.Id).RemainQuota, "queued token reservation is not flushed yet")

	require.NoError(t, MarkBillingRecoveryRefundPending("recovery-batch-fence", nil))
	require.NoError(t, ApplyBillingRecoveryAdjustment("recovery-batch-fence", BillingRecoveryComponentFunding, BillingRecoveryOperationRefund))
	require.NoError(t, ApplyBillingRecoveryAdjustment("recovery-batch-fence", BillingRecoveryComponentToken, BillingRecoveryOperationRefund))

	// The recovery transaction must first materialize the queued -50 deltas,
	// then refund 40. A fenced cache falls back to this 50 balance rather than
	// stale pre-batch database balances of 100 or 60.
	reserved, err = TryReserveUserQuota(user.Id, 51)
	require.NoError(t, err)
	assert.False(t, reserved)
	reserved, err = TryReserveTokenQuota(token.Id, token.Key, 51, false)
	require.NoError(t, err)
	assert.False(t, reserved)
	assert.Equal(t, 50, getUserQuotaFromDB(t, user.Id))
	reloadedToken := getTokenFromDB(t, token.Id)
	assert.Equal(t, 50, reloadedToken.RemainQuota)
	assert.Equal(t, 50, reloadedToken.UsedQuota)

	batchUpdate()
	assert.Equal(t, 50, getUserQuotaFromDB(t, user.Id))
	assert.Equal(t, 50, getTokenFromDB(t, token.Id).RemainQuota)
}

func TestUpdateBillingRecoveryAmountsRejectsTerminalRecoveries(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	for _, status := range []string{BillingRecoveryStatusSettled, BillingRecoveryStatusRefunded} {
		t.Run(status, func(t *testing.T) {
			requestID := "terminal-amounts-" + status
			recovery := BillingRecovery{
				RequestID: requestID, UserID: 1, Source: "wallet",
				FundingAmount: 40, ExtraAmount: 3, TokenAmount: 20,
				PreConsumeState: BillingRecoveryPreConsumeCommitted, Status: status,
			}
			require.NoError(t, db.Create(&recovery).Error)

			err := UpdateBillingRecoveryAmounts(requestID, 99, 1, 2, 3)
			assert.ErrorIs(t, err, ErrBillingRecoveryStateConflict)

			var reloaded BillingRecovery
			require.NoError(t, db.Where("request_id = ?", requestID).First(&reloaded).Error)
			assert.Equal(t, 0, reloaded.SubscriptionID)
			assert.EqualValues(t, 40, reloaded.FundingAmount)
			assert.EqualValues(t, 3, reloaded.ExtraAmount)
			assert.EqualValues(t, 20, reloaded.TokenAmount)
			assert.Equal(t, status, reloaded.Status)
		})
	}
}

func TestBillingRecoveryReserveUsesPersistentAmountAndSubscriptionSettlementDelta(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.Create(&User{Id: 1, Username: "recovery-user", Quota: 100}).Error)
	require.NoError(t, db.Create(&Token{Id: 1, UserId: 1, Key: "reserve-token", RemainQuota: 100}).Error)
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: "reserve-wallet", UserID: 1, TokenID: 1, Source: "wallet", TokenRequired: true})
	require.NoError(t, err)
	require.NoError(t, UpdateBillingRecoveryAmounts("reserve-wallet", 0, 40, 0, 40))
	require.NoError(t, db.Model(&User{}).Where("id = ?", 1).Update("quota", 60).Error)
	require.NoError(t, db.Model(&Token{}).Where("id = ?", 1).Updates(map[string]any{"remain_quota": 60, "used_quota": 40}).Error)

	applied, err := ReserveBillingRecovery("reserve-wallet", 70, 30, false)
	require.NoError(t, err)
	assert.EqualValues(t, 30, applied)
	applied, err = ReserveBillingRecovery("reserve-wallet", 70, 30, false)
	require.NoError(t, err)
	assert.Zero(t, applied)
	applied, err = ReserveBillingRecovery("reserve-wallet", 90, 20, false)
	require.NoError(t, err)
	assert.EqualValues(t, 20, applied)

	var recovery BillingRecovery
	var token Token
	require.NoError(t, db.Where("request_id = ?", "reserve-wallet").First(&recovery).Error)
	require.NoError(t, db.Select("remain_quota", "used_quota").Where("id = ?", 1).First(&token).Error)
	assert.Equal(t, int64(90), recovery.FundingAmount)
	assert.Equal(t, int64(90), recovery.TokenAmount)
	assert.Equal(t, 10, token.RemainQuota)
	assert.Equal(t, 90, token.UsedQuota)

	require.NoError(t, db.Create(&UserSubscription{Id: 1, UserId: 1, AmountTotal: 1000, AmountUsed: 40}).Error)
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: "settle-subscription", UserID: 1, Source: "subscription"})
	require.NoError(t, err)
	require.NoError(t, UpdateBillingRecoveryAmounts("settle-subscription", 1, 40, 0, 0))
	require.NoError(t, UpdateBillingRecoverySettlement("settle-subscription", 10))
	require.NoError(t, ApplyBillingRecoveryAdjustment("settle-subscription", BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
	require.NoError(t, ApplyBillingRecoveryAdjustment("settle-subscription", BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
	require.NoError(t, MarkBillingRecoverySettled("settle-subscription"))
	var subscription UserSubscription
	require.NoError(t, db.Where("id = ?", 1).First(&subscription).Error)
	assert.Equal(t, int64(50), subscription.AmountUsed)
}

func TestListStaleBillingRecoveriesIncludesPreparedRowsForSafeReconciliation(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: "stale-prepared", UserID: 1, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "stale-prepared").Update("updated_at", common.GetTimestamp()-3600).Error)
	rows, err := ListStaleBillingRecoveries(common.GetTimestamp()-30, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "stale-prepared", rows[0].RequestID)
}
