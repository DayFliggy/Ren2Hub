package model

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"

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
		&User{}, &Token{},
		&BillingRecovery{}, &BillingEvent{}, &BillingAttempt{}, &Log{}, &QuotaData{}, &Channel{},
		&BillingPriceVersion{}, &BillingActivePrice{},
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

func seedBillingAccount(t *testing.T, db *gorm.DB, wallet, token common.Quota, unlimited bool) {
	t.Helper()
	require.NoError(t, db.Create(&User{Id: 1, Username: "wallet", Quota: wallet}).Error)
	require.NoError(t, db.Create(&Token{Id: 1, UserId: 1, Key: "billing-test", RemainQuota: token, UnlimitedQuota: unlimited}).Error)
}

func newReservation(t *testing.T, id string) {
	t.Helper()
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: id, UserID: 1, TokenID: 1, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
}

func TestBillingAbortedReservationHasNoPendingDelivery(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 1, 1, false)
	newReservation(t, "unfunded")
	require.ErrorIs(t, Reserve("unfunded", 2), ErrBillingRecoveryFundingQuota)
	require.NoError(t, AbortPreparedBillingRecovery("unfunded", ErrBillingRecoveryFundingQuota))
	require.NoError(t, AbortPreparedBillingRecovery("unfunded", ErrBillingRecoveryFundingQuota))
	metrics, err := ReadBillingMetrics(context.Background())
	require.NoError(t, err)
	assert.Zero(t, metrics.Pending)
	assert.Zero(t, metrics.Reconciliations)
	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "unfunded").First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStateCompleted, recovery.ConsumptionState)
	assert.EqualValues(t, 1, recovery.Version)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.Zero(t, count)
	assertBillingProjection(t, db, 1, 1)
}

func assertBillingProjection(t *testing.T, db *gorm.DB, initialWallet, initialToken common.Quota) {
	t.Helper()
	var user User
	var token Token
	var events []BillingEvent
	require.NoError(t, db.First(&user, 1).Error)
	require.NoError(t, db.First(&token, 1).Error)
	require.NoError(t, db.Find(&events).Error)
	a, r, u := initialWallet, common.Quota(0), common.Quota(0)
	ta, tr, tu := initialToken, common.Quota(0), common.Quota(0)
	for _, event := range events {
		a += event.AvailableDelta
		r += event.ReservedDelta
		u += event.UsedDelta
		ta += event.TokenAvailableDelta
		tr += event.TokenReservedDelta
		tu += event.TokenUsedDelta
	}
	assert.Equal(t, []common.Quota{a, r, u}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
	assert.Equal(t, []common.Quota{ta, tr, tu}, []common.Quota{token.RemainQuota, token.ReservedQuota, token.UsedQuota})
}

func TestBillingReserveSettleConservationAndDebt(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "bill")
	require.NoError(t, Reserve("bill", 40))
	require.NoError(t, Reserve("bill", 40))
	require.NoError(t, IncreaseReservation("bill", 70))
	require.NoError(t, IncreaseReservation("bill", 50))
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 30, user.Quota)
	assert.EqualValues(t, 70, user.ReservedQuota)
	assert.Zero(t, user.UsedQuota)
	require.ErrorIs(t, IncreaseReservation("bill", 101), ErrBillingRecoveryFundingQuota)
	require.NoError(t, UpdateBillingRecoverySettlement("bill", 60))
	require.NoError(t, Settle("bill", 130))
	require.NoError(t, Settle("bill", 130))
	require.ErrorIs(t, Settle("bill", 129), ErrBillingEventConflict)
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, -30, user.Quota)
	assert.Zero(t, user.ReservedQuota)
	assert.EqualValues(t, 130, user.UsedQuota)
	newReservation(t, "debt-next")
	require.ErrorIs(t, Reserve("debt-next", 1), ErrBillingRecoveryFundingQuota)
	assertBillingProjection(t, db, 100, 100)
}

func TestBillingReserveAtomicTokenFailureAndOverflow(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, common.Quota(math.MaxInt32)+100, 5, false)
	newReservation(t, "bill")
	require.ErrorIs(t, Reserve("bill", 10), ErrBillingRecoveryTokenQuota)
	assertBillingProjection(t, db, common.Quota(math.MaxInt32)+100, 5)
	require.NoError(t, db.Model(&User{}).Where("id = 1").Update("available_quota", common.Quota(math.MaxInt64)).Error)
	_, err := ApplyWalletAdjustment(WalletAdjustment{EventID: "overflow", UserID: 1, Delta: 1})
	require.ErrorIs(t, err, common.ErrQuotaOverflow)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestBillingEventFailureRollsBackBothAccounts(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "bill")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("event_fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_events" {
			tx.AddError(assert.AnError)
		}
	}))
	require.Error(t, Reserve("bill", 40))
	assertBillingProjection(t, db, 100, 100)
	require.NoError(t, db.Callback().Create().Remove("event_fail"))
	require.NoError(t, Reserve("bill", 40))
	require.NoError(t, MarkBillingRecoveryRefundPending("bill", nil))
	require.NoError(t, Release("bill"))
	require.NoError(t, Release("bill"))
	assertBillingProjection(t, db, 100, 100)
}

func TestBillingUnlimitedTokenStillTracksReservationAndConsumption(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 0, true)
	newReservation(t, "bill")
	require.NoError(t, Reserve("bill", 40))
	var token Token
	require.NoError(t, db.First(&token, 1).Error)
	assert.Zero(t, token.RemainQuota)
	assert.EqualValues(t, 40, token.ReservedQuota)
	assert.Zero(t, token.UsedQuota)
	require.NoError(t, UpdateBillingRecoverySettlement("bill", 10))
	require.NoError(t, Settle("bill", 50))
	assertBillingProjection(t, db, 100, 0)
	encoded, err := common.Marshal(token)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"available_quota":null`)
}

func TestBillingConcurrentReservationsCannotOverspend(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	seedBillingAccount(t, db, 100, 100, false)
	for i := 0; i < 20; i++ {
		newReservation(t, fmt.Sprintf("request-%d", i))
	}
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) { defer wg.Done(); results <- Reserve(fmt.Sprintf("request-%d", id), 10) }(i)
	}
	wg.Wait()
	close(results)
	accepted := 0
	for err := range results {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, ErrBillingRecoveryFundingQuota)
		}
	}
	assert.Equal(t, 10, accepted)
	assertBillingProjection(t, db, 100, 100)
}

func TestBillingManualResolutionIdempotencyAndVersion(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "bill")
	require.NoError(t, Reserve("bill", 40))
	var row BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "bill").First(&row).Error)
	command := BillingResolution{OperationID: "manual-1", ExpectedVersion: row.Version, Action: "settle", Quota: 25, Reason: "Provider usage confirmed"}
	_, err := ResolveBillingReconciliation(context.Background(), row.ID, 1, command)
	require.NoError(t, err)
	_, err = ResolveBillingReconciliation(context.Background(), row.ID, 1, command)
	require.NoError(t, err)
	command.Quota = 26
	_, err = ResolveBillingReconciliation(context.Background(), row.ID, 1, command)
	require.ErrorIs(t, err, ErrBillingEventConflict)
	assertBillingProjection(t, db, 100, 100)
}
