package model

import (
	"context"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupBillingRecoveryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("CRYPTO_SECRET", "billing-test-persistent-key")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&User{}, &Token{}, &Option{},
		&BillingRecovery{}, &BillingEvent{}, &BillingAttempt{}, &Log{}, &QuotaData{}, &Channel{},
		&BillingPriceVersion{},
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
	backlog, err := ReadBillingRecoveryBacklog(context.Background(), 1<<62)
	require.NoError(t, err)
	assert.Zero(t, backlog.Pending)
	rows, err := ListStaleBillingRecoveries(1<<62, 100)
	require.NoError(t, err)
	assert.Empty(t, rows)
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
	consumption, err := bindConsumptionEvidence("bill", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 130}})
	require.NoError(t, err)
	require.NoError(t, PrepareBillingRecoveryConsumption("bill", consumption))
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
	consumption, err := bindConsumptionEvidence("bill", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 50}})
	require.NoError(t, err)
	require.NoError(t, PrepareBillingRecoveryConsumption("bill", consumption))
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

// The bill-first fallback charges exactly the attempt's persisted pre-send
// estimate. An attempt without that estimate was never sent and is released.
func TestFrozenEstimateSettlesOnlyTheAttemptEstimate(t *testing.T) {
	t.Run("persisted estimate", func(t *testing.T) {
		db := setupBillingRecoveryTestDB(t)
		seedBillingAccount(t, db, 100, 100, false)
		newReservation(t, "frozen")
		require.NoError(t, Reserve("frozen", 40))
		consumption, err := bindConsumptionEvidence("frozen", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
		require.NoError(t, err)
		var attempt BillingAttempt
		require.NoError(t, db.First(&attempt, "id = ?", consumption.AttemptID).Error)
		var quote billing.Quote
		require.NoError(t, common.UnmarshalJsonStr(attempt.Quote, &quote))
		var estimate billing.Usage
		require.NoError(t, common.UnmarshalJsonStr(string(attempt.EstimateUsage), &estimate))
		forged := estimate
		forged.Quantities = map[string]string{"calls": "2"}
		forgedUsage, forgedCharge, err := EvaluateBilling(&quote, forged, billingexpr.RequestInput{}, "frozen", consumption.AttemptID, "estimate")
		require.NoError(t, err)
		consumption.Usage, consumption.Charge, consumption.Log.Quota = &forgedUsage, forgedCharge, forgedCharge.FinalQuota
		require.ErrorIs(t, PrepareBillingRecoveryConsumption("frozen", consumption), ErrBillingEventConflict, "an estimate label cannot carry another charge")

		require.NoError(t, PrepareFrozenEstimateConsumption("frozen", "", Log{}, true))
		require.NoError(t, Settle("frozen", 30))
		var settled BillingRecovery
		require.NoError(t, db.First(&settled, "request_id = ?", "frozen").Error)
		assert.Equal(t, BillingRecoveryStatusSettled, settled.Status)
		assert.JSONEq(t, attempt.EstimateCharge, settled.ChargeSnapshot)
		var user User
		require.NoError(t, db.First(&user, 1).Error)
		assert.Equal(t, []common.Quota{70, 0, 30}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
		assertBillingProjection(t, db, 100, 100)
	})

	t.Run("attempt without estimate", func(t *testing.T) {
		db := setupBillingRecoveryTestDB(t)
		seedBillingAccount(t, db, 100, 100, false)
		newReservation(t, "unsent")
		require.NoError(t, Reserve("unsent", 40))
		version, err := billing.NewModelPriceVersion("fixture", billing.PricingValues{"ModelPrice": "0.00006"}, false)
		require.NoError(t, err)
		quote, err := publishedBillingTestQuote(version, "1", nil, time.Unix(7200, 0))
		require.NoError(t, err)
		require.NoError(t, PrepareBillingAttempt(context.Background(), "unsent", "unsent:attempt", 0, quote))
		require.ErrorIs(t, PrepareFrozenEstimateConsumption("unsent", "", Log{}, true), ErrBillingEstimateMissing)
		var recovery BillingRecovery
		require.NoError(t, db.First(&recovery, "request_id = ?", "unsent").Error)
		assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
		var pending BillingConsumption
		require.NoError(t, common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &pending))
		assert.Equal(t, "unsent:attempt", pending.AttemptID)
		assert.Empty(t, recovery.ChargeSnapshot)
		assert.Empty(t, recovery.ConsumptionFingerprint, "a pending attempt without an estimate is not a charge")
		require.NoError(t, MarkBillingRecoveryRefundPending("unsent", nil))
		require.NoError(t, Release("unsent"))
		var user User
		require.NoError(t, db.First(&user, 1).Error)
		assert.Equal(t, []common.Quota{100, 0, 0}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
		assertBillingProjection(t, db, 100, 100)
	})
}

func TestCorruptQuoteCannotAuthorizeUnsentRelease(t *testing.T) {
	for _, snapshot := range []string{"{broken", "{}", ""} {
		t.Run(snapshot, func(t *testing.T) {
			db := setupBillingRecoveryTestDB(t)
			seedBillingAccount(t, db, 100, 100, false)
			newReservation(t, "corrupt-quote")
			require.NoError(t, Reserve("corrupt-quote", 40))
			_, err := bindConsumptionEvidence("corrupt-quote", BillingConsumption{Log: Log{UserId: 1, TokenId: 1, Quota: 30}})
			require.NoError(t, err)
			require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", "corrupt-quote").Update("quote_snapshot", snapshot).Error)
			require.ErrorIs(t, PrepareFrozenEstimateConsumption("corrupt-quote", "", Log{}, true), billing.ErrEvaluationEvidence)
			require.ErrorIs(t, MarkUnsentBillingRecoveryRefundPending("corrupt-quote"), billing.ErrEvaluationEvidence)
			var recovery BillingRecovery
			require.NoError(t, db.First(&recovery, "request_id = ?", "corrupt-quote").Error)
			assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
			var user User
			require.NoError(t, db.First(&user, 1).Error)
			assert.EqualValues(t, 40, user.ReservedQuota)
			assert.Zero(t, user.UsedQuota)
		})
	}
}
