package model

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/shopspring/decimal"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func publishedBillingTestQuote(version billing.PriceVersion, ratio string, attributes map[string]interface{}, at time.Time) (*billing.Quote, error) {
	published, err := StoreBillingPriceEvidence(context.Background(), version)
	if err != nil {
		return nil, err
	}
	return billing.NewQuote(*published, ratio, attributes, at)
}

// Log-delivery fixtures still pass through a real frozen billing contract.
func bindConsumptionEvidence(requestID string, consumption BillingConsumption) (BillingConsumption, error) {
	name := consumption.Log.ModelName
	if name == "" {
		name = "fixture"
	}
	var recovery BillingRecovery
	if err := DB.First(&recovery, "request_id = ?", requestID).Error; err != nil {
		return consumption, err
	}
	values := billing.PricingValues{"ModelPrice": decimal.NewFromInt(int64(consumption.Log.Quota)).Div(decimal.NewFromInt(500000)).String()}
	overrun := consumption.Log.Quota > recovery.FundingAmount
	if overrun {
		values = billing.PricingValues{"ModelRatio": 1.0, "CompletionRatio": 0.0}
	}
	version, err := billing.NewModelPriceVersion(name, values, false)
	if err != nil {
		return consumption, err
	}
	quote, err := publishedBillingTestQuote(version, "1", nil, time.Unix(7200, 0))
	if err != nil {
		return consumption, err
	}
	if err = PrepareBillingAttempt(context.Background(), requestID, requestID+":attempt", consumption.Log.ChannelId, quote); err != nil {
		return consumption, err
	}
	consumption.Usage = &billing.Usage{Complete: true, Source: "provider", Terminal: "completed", Quantities: map[string]string{"calls": "1"}, InputTokens: int64(consumption.Log.PromptTokens), OutputTokens: int64(consumption.Log.CompletionTokens)}
	if overrun {
		consumption.Usage.InputTokens = int64(consumption.Log.Quota)
		consumption.Usage.OutputTokens = 0
	}
	consumption.AttemptID = requestID + ":attempt"
	estimate := *consumption.Usage
	estimate.Estimated = true
	estimate.Complete = false
	estimate.Terminal = ""
	estimate.Source = "request_estimate"
	if overrun {
		estimate.InputTokens = int64(recovery.FundingAmount)
	}
	estimate, estimateCharge, err := EvaluateBilling(quote, estimate, billingexpr.RequestInput{}, requestID, consumption.AttemptID, "estimate")
	if err != nil {
		return consumption, err
	}
	if err = PersistBillingEstimate(consumption.AttemptID, estimate, estimateCharge); err != nil {
		return consumption, err
	}
	usage, charge, err := EvaluateBilling(quote, *consumption.Usage, billingexpr.RequestInput{}, requestID, consumption.AttemptID, "settlement")
	consumption.Usage = &usage
	consumption.Charge = charge
	return consumption, err
}

func setupBillingConsumptionTest(t *testing.T) (*gorm.DB, *gorm.DB, BillingConsumption) {
	t.Helper()
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &QuotaData{}))
	logs, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, logs.AutoMigrate(&Log{}))
	previousLogs := LOG_DB
	LOG_DB = logs
	t.Cleanup(func() { LOG_DB = previousLogs })
	require.NoError(t, db.Create(&User{Id: 1, Username: "consumption", Quota: 100}).Error)
	require.NoError(t, db.Create(&Token{Id: 2, UserId: 1, Key: "consumption-test", RemainQuota: 100}).Error)
	require.NoError(t, db.Create(&Channel{Id: 3, Name: "consumption"}).Error)
	_, err = EnsureBillingRecovery(BillingRecoveryInput{RequestID: "consume-once", UserID: 1, TokenID: 2, Source: "wallet", TokenRequired: true})
	require.NoError(t, err)
	require.NoError(t, Reserve("consume-once", 40))
	consumption := BillingConsumption{
		Log: Log{UserId: 1, TokenId: 2, ChannelId: 3, Username: "consumption", CreatedAt: 7200, Type: LogTypeConsume,
			ModelName: "test-model", Quota: 30, PromptTokens: 10, CompletionTokens: 20},
		CountUsage: true, LogEnabled: true, ExportEnabled: true, NodeName: "test-node",
	}
	consumption, err = bindConsumptionEvidence("consume-once", consumption)
	require.NoError(t, err)
	require.NoError(t, PrepareBillingRecoveryConsumption("consume-once", consumption))
	return db, logs, consumption
}

func completeConsumptionFunds(t *testing.T) {
	t.Helper()
	require.NoError(t, Settle("consume-once", 30))
	require.NoError(t, Settle("consume-once", 30))
}

func TestAccountDeletionWaitsForSettledConsumptionDelivery(t *testing.T) {
	db, _, _ := setupBillingConsumptionTest(t)
	require.NoError(t, db.AutoMigrate(&UserSession{}))
	completeConsumptionFunds(t)
	user := User{Id: 1}
	require.ErrorContains(t, user.Delete(), "unresolved billing")
	require.ErrorContains(t, user.HardDelete(), "unresolved billing")
	require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
	var restored User
	require.NoError(t, db.First(&restored, 1).Error)
	assert.Equal(t, 1, restored.RequestCount)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return ensureNoActiveAccountBillingTx(tx, 1, 0)
	}))
}

func TestBillingConsumptionPreservesSettlementIntentAndRejectsConflictingUsage(t *testing.T) {
	db, _, consumption := setupBillingConsumptionTest(t)
	assert.ErrorIs(t, ApplyBillingRecoveryConsumption("consume-once"), ErrBillingRecoveryStateConflict)
	assert.ErrorIs(t, MarkBillingRecoveryRefundPending("consume-once", nil), ErrBillingRecoveryStateConflict)
	require.NoError(t, PrepareBillingRecoveryConsumption("consume-once", consumption))
	consumption.Log.Quota++
	assert.ErrorIs(t, PrepareBillingRecoveryConsumption("consume-once", consumption), ErrBillingEventConflict)
	assert.ErrorIs(t, UpdateBillingRecoverySettlement("consume-once", 999), ErrBillingRecoveryStateConflict)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 60, user.Quota)
	assert.Zero(t, user.RequestCount)
}

func TestBillingConsumptionRecoversWithoutDuplicateCountersOrLogs(t *testing.T) {
	for _, failure := range []string{"statistics", "log_insert", "completion_marker"} {
		t.Run(failure, func(t *testing.T) {
			db, logs, _ := setupBillingConsumptionTest(t)
			completeConsumptionFunds(t)
			callbackName := "test:billing-consumption-" + failure
			switch failure {
			case "statistics":
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
					if tx.Statement.Table == "channels" {
						tx.AddError(assert.AnError)
					}
				}))
			case "log_insert":
				require.NoError(t, logs.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) { tx.AddError(assert.AnError) }))
			case "completion_marker":
				require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
					values, ok := tx.Statement.Dest.(map[string]any)
					if ok && values["consumption_state"] == BillingRecoveryStateCompleted {
						tx.AddError(assert.AnError)
					}
				}))
			}
			require.Error(t, ApplyBillingRecoveryConsumption("consume-once"))
			var pending BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "consume-once").First(&pending).Error)
			assert.EqualValues(t, BillingRecoveryStatusSettled, pending.Status)
			assert.EqualValues(t, BillingRecoveryStatePending, pending.ConsumptionState)
			assert.NotEmpty(t, pending.ConsumptionError)
			assert.ErrorIs(t, MarkBillingRecoveryRefundPending("consume-once", nil), ErrBillingRecoveryStateConflict)
			if failure == "log_insert" {
				require.NoError(t, logs.Callback().Create().Remove(callbackName))
			} else {
				require.NoError(t, db.Callback().Update().Remove(callbackName))
			}
			for i := 0; i < 2; i++ {
				require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
			}
			var user User
			var token Token
			var channel Channel
			var quotaData QuotaData
			var storedLogs []Log
			require.NoError(t, db.First(&user, 1).Error)
			require.NoError(t, db.First(&token, 2).Error)
			require.NoError(t, db.First(&channel, 3).Error)
			require.NoError(t, db.First(&quotaData).Error)
			require.NoError(t, logs.Find(&storedLogs).Error)
			assert.EqualValues(t, 70, user.Quota)
			assert.EqualValues(t, 30, user.UsedQuota)
			assert.EqualValues(t, 1, user.RequestCount)
			assert.EqualValues(t, 70, token.RemainQuota)
			assert.EqualValues(t, 30, token.UsedQuota)
			assert.EqualValues(t, int64(30), channel.UsedQuota)
			assert.EqualValues(t, 30, quotaData.Quota)
			assert.EqualValues(t, 1, quotaData.Count)
			assert.EqualValues(t, 30, quotaData.TokenUsed)
			require.Len(t, storedLogs, 1)
			assert.EqualValues(t, "consume-once", storedLogs[0].RequestId)
			assert.EqualValues(t, 30, storedLogs[0].Quota)
			var complete BillingRecovery
			require.NoError(t, db.Where("request_id = ?", "consume-once").First(&complete).Error)
			assert.EqualValues(t, BillingRecoveryStateCompleted, complete.ConsumptionState)
			assert.Empty(t, complete.ConsumptionError)
		})
	}
}

func TestBillingLogIdempotencyAllowsLegacyNullKeys(t *testing.T) {
	_, logs, _ := setupBillingConsumptionTest(t)
	require.NoError(t, logs.Create(&Log{Type: LogTypeSystem}).Error)
	require.NoError(t, logs.Create(&Log{Type: LogTypeSystem}).Error)
	completeConsumptionFunds(t)
	require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
	require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
	var count int64
	require.NoError(t, logs.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, int64(3), count)
}

// A request whose settlement intent could not be persisted keeps its
// reservation and stays listed for automatic recovery; nothing is delivered.
func TestBillingPreparationFailureStaysListedForAutomaticRecovery(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	seedBillingAccount(t, db, 100, 100, false)
	newReservation(t, "unprepared")
	require.NoError(t, Reserve("unprepared", 40))
	require.NoError(t, RecordBillingConsumptionPreparationFailure("unprepared", assert.AnError))
	require.ErrorContains(t, ApplyBillingRecoveryConsumption("unprepared"), "not final")
	rows, err := ListStaleBillingRecoveries(1<<62, 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "unprepared", rows[0].RequestID)
	backlog, err := ReadBillingRecoveryBacklog(context.Background(), 1<<62)
	require.NoError(t, err)
	assert.EqualValues(t, 1, backlog.Pending)
	var pending BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "unprepared").First(&pending).Error)
	assert.Equal(t, BillingRecoveryStatusActive, pending.Status)
	assert.Equal(t, BillingRecoveryStateUnprepared, pending.ConsumptionState)
	assert.NotEmpty(t, pending.ConsumptionError)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.Equal(t, []common.Quota{60, 40, 0}, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
	assert.Zero(t, user.RequestCount)
}

// Exercise the ClickHouse delivery policy with an isolated SQL-backed store.
// This verifies the unknown-write fence, not a real ClickHouse driver's behavior.
func TestBillingAmbiguousClickHouseWriteIsConfirmedBeforeRetry(t *testing.T) {
	db, logs, consumption := setupBillingConsumptionTest(t)
	completeConsumptionFunds(t)
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)
	writes := 0
	require.NoError(t, logs.Callback().Create().Before("gorm:create").Register("unknown_insert", func(tx *gorm.DB) { writes++; tx.AddError(assert.AnError) }))
	require.ErrorContains(t, ApplyBillingRecoveryConsumption("consume-once"), "outcome is unknown")
	require.NoError(t, logs.Callback().Create().Remove("unknown_insert"))
	require.ErrorContains(t, ApplyBillingRecoveryConsumption("consume-once"), "outcome is unknown")
	assert.Equal(t, 1, writes)
	var count int64
	require.NoError(t, logs.Model(&Log{}).Count(&count).Error)
	assert.Zero(t, count)
	backlog, err := ReadBillingRecoveryBacklog(context.Background(), 1<<62)
	require.NoError(t, err)
	assert.EqualValues(t, 1, backlog.Pending)
	var pending BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "consume-once").First(&pending).Error)
	assert.Equal(t, BillingRecoveryStatePending, pending.ConsumptionState)
	assert.NotEmpty(t, pending.ConsumptionError)
	// An initially invisible insert becomes visible by its stable event ID.
	eventID := "consume-once:settle"
	consumption.Log.RequestId, consumption.Log.BillingEventID = "consume-once", &eventID
	require.NoError(t, logs.Create(&consumption.Log).Error)
	for range 2 {
		require.NoError(t, ApplyBillingRecoveryConsumption("consume-once"))
	}
	require.NoError(t, logs.Model(&Log{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 70, user.Quota)
	assert.EqualValues(t, 30, user.UsedQuota)
	backlog, err = ReadBillingRecoveryBacklog(context.Background(), 1<<62)
	require.NoError(t, err)
	assert.Zero(t, backlog.Pending)
	var delivered BillingRecovery
	require.NoError(t, db.Where("request_id = ?", "consume-once").First(&delivered).Error)
	assert.Equal(t, BillingRecoveryStateCompleted, delivered.ConsumptionState)
	assert.Empty(t, delivered.ConsumptionError)
}
