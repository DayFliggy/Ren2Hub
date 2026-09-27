package model

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMidjourneyCompletionRecoveryAcrossDatabases(t *testing.T) {
	for _, database := range []struct {
		kind common.DatabaseType
		env  string
	}{{common.DatabaseTypeSQLite, ""}, {common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"}, {common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"}} {
		t.Run(string(database.kind), func(t *testing.T) {
			dsn := os.Getenv(database.env)
			if database.env != "" && dsn == "" {
				t.Skip(database.env + " must target an isolated database")
			}
			db, err := gatewayStartupDatabase(t, database.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLogDB := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			previousRedis, previousBatch := common.RedisEnabled, common.BatchUpdateEnabled
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(database.kind, database.kind)
			common.RedisEnabled, common.BatchUpdateEnabled = false, false
			initCol()
			connection, err := db.DB()
			require.NoError(t, err)
			if database.kind == common.DatabaseTypeSQLite {
				connection.SetMaxOpenConns(1)
			}
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLogDB
				common.SetDatabaseTypes(previousKind, previousLogKind)
				common.RedisEnabled, common.BatchUpdateEnabled = previousRedis, previousBatch
				initCol()
				require.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&Midjourney{}, &User{}, &Token{}, &Channel{}, &Log{}, &QuotaData{}, &BillingRecovery{}, &BillingEvent{}, &BillingAttempt{}))
			t.Run("accepted-submission", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "accepted", false, true)
				testMidjourneyAcceptedSubmission(t, fixture)
			})
			t.Run("concurrent-owner", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "owner", false, true)
				testMidjourneyConcurrentOwner(t, fixture)
			})
			for _, status := range []string{"SUCCESS", "FAILURE"} {
				t.Run("wallet/"+status, func(t *testing.T) {
					fixture := newMidjourneyCompletionFixture(t, db, "wallet"+status, false, true)
					testMidjourneyCompletionRecovery(t, fixture, status)
				})
				t.Run("retired-source/"+status, func(t *testing.T) {
					fixture := newMidjourneyCompletionFixture(t, db, "retired-"+status, false, true)
					testMidjourneyCompletionRejectsRetiredSource(t, fixture, status)
				})
			}
			for _, test := range []struct {
				name          string
				legacy        bool
				tokenRequired bool
			}{{"legacy-token", true, true}, {"legacy-wallet-only", true, false}, {"playground", false, false}} {
				t.Run(test.name, func(t *testing.T) {
					fixture := newMidjourneyCompletionFixture(t, db, test.name, test.legacy, test.tokenRequired)
					if test.legacy {
						fixture.task.Status = "FAILURE"
						won, err := PersistMidjourneyCompletion(context.Background(), &fixture.task, "IN_PROGRESS", fixture.consumption)
						require.ErrorIs(t, err, ErrBillingRecoveryStateConflict)
						assert.False(t, won)
						assertTaskAccountProjection(t, db, fixture.user.Id, fixture.token.Id, 100, 0, 0)
						var persisted Midjourney
						require.NoError(t, db.First(&persisted, fixture.task.Id).Error)
						assert.Equal(t, "IN_PROGRESS", persisted.Status)
						var events int64
						require.NoError(t, db.Model(&BillingEvent{}).Where("user_id = ?", fixture.user.Id).Count(&events).Error)
						assert.Zero(t, events, "missing reservation cannot synthesize a credit event")
					} else {
						testMidjourneyCompletionRecovery(t, fixture, "FAILURE")
					}
				})
			}
			t.Run("concurrent-terminal", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "concurrent", false, true)
				testMidjourneyConcurrentCompletion(t, fixture)
			})
			t.Run("submission-pending", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "pending", false, true)
				require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", fixture.task.PrivateData.SubmissionRequestID).
					Update("consumption_state", BillingRecoveryStatePending).Error)
				fixture.task.Status = "FAILURE"
				won, err := PersistMidjourneyCompletion(context.Background(), &fixture.task, "IN_PROGRESS", fixture.consumption)
				require.ErrorIs(t, err, ErrBillingRecoveryStateConflict)
				assert.False(t, won)
				var persisted Midjourney
				require.NoError(t, db.First(&persisted, fixture.task.Id).Error)
				assert.Equal(t, "IN_PROGRESS", persisted.Status)
			})
		})
	}
}

func testMidjourneyCompletionRejectsRetiredSource(t *testing.T, fixture midjourneyCompletionFixture, status string) {
	t.Helper()
	db, task := fixture.db, fixture.task
	task.PrivateData.BillingSource = "subscription"
	require.NoError(t, db.Model(&task).Update("private_data", task.PrivateData).Error)
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", task.PrivateData.SubmissionRequestID).Update("source", "subscription").Error)
	task.Status, task.Progress = status, "100%"
	for range 2 {
		won, err := PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
		require.ErrorIs(t, err, ErrUnsupportedBillingSource)
		assert.False(t, won)
	}
	var persisted Midjourney
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.Equal(t, "IN_PROGRESS", persisted.Status)
	assert.Equal(t, "45%", persisted.Progress)
	assert.EqualValues(t, 30, persisted.Quota)
	assert.Equal(t, "subscription", persisted.PrivateData.BillingSource)
	require.NoError(t, db.First(&fixture.user, fixture.user.Id).Error)
	require.NoError(t, db.First(&fixture.token, fixture.token.Id).Error)
	require.NoError(t, db.First(&fixture.channel, fixture.channel.Id).Error)
	assert.EqualValues(t, 70, fixture.user.Quota)
	assert.Zero(t, fixture.user.UsedQuota)
	assert.Zero(t, fixture.user.RequestCount)
	assert.EqualValues(t, 70, fixture.token.RemainQuota)
	assert.Zero(t, fixture.token.UsedQuota)
	assert.Zero(t, fixture.channel.UsedQuota)
	var count int64
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", MidjourneyBillingRequestID(task.Id)).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&Log{}).Where("user_id = ?", fixture.user.Id).Count(&count).Error)
	assert.Zero(t, count)
}

func newMidjourneySubmission(t *testing.T, fixture midjourneyCompletionFixture, requestID string) (Midjourney, BillingConsumption) {
	t.Helper()
	task := fixture.task
	task.Id, task.MjId, task.Status = 0, "accepted-provider-id", "SUBMITTED"
	task.PrivateData.SubmissionRequestID, task.PrivateData.CredentialVersion = requestID, 1
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: requestID, UserID: task.UserId, TokenID: task.PrivateData.TokenId, TokenRequired: fixture.tokenRequired, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, Reserve(requestID, task.Quota))
	require.NoError(t, PrepareBillingAttempt(context.Background(), requestID, requestID+":attempt", task.ChannelId, fixture.quote))
	consumption := fixture.consumption
	consumption.Log.Quota = task.Quota
	return task, consumption
}

func testMidjourneyAcceptedSubmission(t *testing.T, fixture midjourneyCompletionFixture) {
	t.Helper()
	task, consumption := newMidjourneySubmission(t, fixture, "mj-accepted-new")
	db := fixture.db
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("mj_submit_fault", func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("mj_submit_fault") })
	require.ErrorIs(t, PersistAcceptedMidjourney(&task, task.PrivateData.SubmissionRequestID, consumption), assert.AnError)
	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
	assert.Empty(t, recovery.ConsumptionSnapshot)
	require.NoError(t, db.Callback().Create().Remove("mj_submit_fault"))
	require.NoError(t, PersistAcceptedMidjourney(&task, task.PrivateData.SubmissionRequestID, consumption))
	id := task.Id
	require.NoError(t, PersistAcceptedMidjourney(&task, task.PrivateData.SubmissionRequestID, consumption))
	assert.Equal(t, id, task.Id)
	var count int64
	require.NoError(t, db.Model(&Midjourney{}).Where("user_id = ? AND mj_id = ?", task.UserId, task.MjId).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
	assert.Equal(t, BillingRecoveryStateTaskPending, recovery.ConsumptionState)
	assert.NotEmpty(t, recovery.ConsumptionSnapshot)
	owned, err := GetOwnedMidjourneyTask(context.Background(), task.UserId, task.PrivateData.TokenId, task.MjId)
	require.NoError(t, err)
	assert.Equal(t, id, owned.Id)
	assert.Equal(t, task.PrivateData, owned.PrivateData)
	_, err = GetOwnedMidjourneyTask(context.Background(), task.UserId, task.PrivateData.TokenId+100, task.MjId)
	require.ErrorIs(t, err, ErrGatewayStateUnknown)
}

func testMidjourneyConcurrentOwner(t *testing.T, fixture midjourneyCompletionFixture) {
	t.Helper()
	first, firstConsumption := newMidjourneySubmission(t, fixture, "mj-owner-first")
	second, secondConsumption := newMidjourneySubmission(t, fixture, "mj-owner-second")
	second.PrivateData.CredentialID = "another-account"
	results := make(chan error, 2)
	start := make(chan struct{})
	go func() {
		<-start
		results <- PersistAcceptedMidjourney(&first, first.PrivateData.SubmissionRequestID, firstConsumption)
	}()
	go func() {
		<-start
		results <- PersistAcceptedMidjourney(&second, second.PrivateData.SubmissionRequestID, secondConsumption)
	}()
	close(start)
	winners := 0
	for range 2 {
		if err := <-results; err == nil {
			winners++
		} else {
			require.ErrorIs(t, err, ErrGatewayStateUnknown)
		}
	}
	assert.Equal(t, 1, winners, "a public provider ID cannot expose two accounts to the same Token")
	owner, err := GetOwnedMidjourneyTask(context.Background(), first.UserId, first.PrivateData.TokenId, first.MjId)
	require.NoError(t, err)
	assert.Contains(t, []string{first.PrivateData.CredentialID, second.PrivateData.CredentialID}, owner.PrivateData.CredentialID)
}

type midjourneyCompletionFixture struct {
	db            *gorm.DB
	task          Midjourney
	consumption   BillingConsumption
	user          User
	token         Token
	channel       Channel
	quote         *billing.Quote
	tokenRequired bool
}

func newMidjourneyCompletionFixture(t *testing.T, db *gorm.DB, identity string, legacy, tokenRequired bool) midjourneyCompletionFixture {
	t.Helper()
	fixture := midjourneyCompletionFixture{db: db, tokenRequired: tokenRequired}
	fixture.user = User{Username: "mj-" + identity, AffCode: "mj-" + identity, Quota: 100}
	require.NoError(t, db.Create(&fixture.user).Error)
	fixture.token = Token{UserId: fixture.user.Id, Key: "mj-" + identity, RemainQuota: 100}
	require.NoError(t, db.Create(&fixture.token).Error)
	fixture.channel = Channel{Name: "mj-" + identity}
	require.NoError(t, db.Create(&fixture.channel).Error)
	quote, err := billing.NewQuote(billing.PriceVersion{Model: "mj_imagine", ScopeKind: billing.PriceScopeChannel,
		ChannelID: fixture.channel.Id, Mode: billing.ModeUnitPrice,
		Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.00006"}}}, "1", nil, time.Unix(7200, 0))
	require.NoError(t, err)
	fixture.quote = quote
	fixture.task = Midjourney{MjId: "provider-" + identity, UserId: fixture.user.Id, ChannelId: fixture.channel.Id,
		Quota: 30, Status: "IN_PROGRESS", Progress: "45%", Action: "IMAGINE"}
	logTokenID := fixture.token.Id
	if legacy {
		logTokenID = 0
		if tokenRequired {
			fixture.task.TokenId, logTokenID = fixture.token.Id, fixture.token.Id
		}
		if !tokenRequired {
			// Older rows may route through a different channel than the one
			// recorded when their initial accounting actually succeeded.
			routeChannel := Channel{Name: "mj-route-" + identity}
			require.NoError(t, db.Create(&routeChannel).Error)
			fixture.task.ChannelId, fixture.task.BillingChannelId = routeChannel.Id, fixture.channel.Id
		}
	} else {
		fixture.task.PrivateData = TaskPrivateData{TokenId: fixture.token.Id, BillingSource: "wallet",
			SubmissionRequestID: "mj-submission-" + identity, CredentialID: "local-credential", EndpointID: "local-endpoint"}
		_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: fixture.task.PrivateData.SubmissionRequestID, UserID: fixture.user.Id, TokenID: fixture.token.Id, TokenRequired: tokenRequired, Source: "wallet"})
		require.NoError(t, err)
		require.NoError(t, Reserve(fixture.task.PrivateData.SubmissionRequestID, 30))
		require.NoError(t, PrepareBillingAttempt(context.Background(), fixture.task.PrivateData.SubmissionRequestID,
			fixture.task.PrivateData.SubmissionRequestID+":attempt", fixture.channel.Id, fixture.quote))
	}
	require.NoError(t, db.Create(&fixture.task).Error)
	usage := billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: "provider", Complete: true}
	charge, err := billing.Calculate(fixture.quote, usage)
	require.NoError(t, err)
	require.EqualValues(t, 30, charge.FinalQuota)
	fixture.consumption = BillingConsumption{Log: Log{UserId: fixture.user.Id, TokenId: logTokenID, ChannelId: fixture.channel.Id,
		Username: fixture.user.Username, ModelName: "mj_imagine", CreatedAt: 7200}, CountUsage: true, LogEnabled: true, ExportEnabled: true,
		Usage: &usage, Charge: charge}
	if !legacy {
		accepted := fixture.consumption
		accepted.Log.Quota = fixture.task.Quota
		require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
			return prepareTaskAcceptedBillingTx(tx, fixture.task.PrivateData.SubmissionRequestID, accepted)
		}))
	}
	return fixture
}

func testMidjourneyCompletionRecovery(t *testing.T, fixture midjourneyCompletionFixture, status string) {
	t.Helper()
	db, task := fixture.db, fixture.task
	task.Status, task.Progress, task.FinishTime = status, "100%", 7300
	task.ImageUrl = "https://result.example.invalid/mj"
	task.Buttons = `[{"label":"V1"}]`
	task.Properties = `{"seed":"123"}`
	task.PrivateData.CredentialID = "must-not-replace-original"
	actual := common.Quota(30)
	release := status == "FAILURE"
	if release {
		actual = 0
		task.FailReason = "provider confirmed task failure"
		fixture.consumption.Charge = nil
		fixture.consumption.Usage = nil
	}
	requestID := fixture.task.PrivateData.SubmissionRequestID
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("mj_completion_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			tx.AddError(assert.AnError)
		}
	}))
	won, err := PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
	require.ErrorIs(t, err, assert.AnError)
	assert.False(t, won)
	require.NoError(t, db.Callback().Update().Remove("mj_completion_failure"))
	var persisted Midjourney
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.Equal(t, "IN_PROGRESS", persisted.Status)
	var recovery BillingRecovery
	require.NoError(t, db.Where("request_id = ?", requestID).First(&recovery).Error)
	assert.Equal(t, BillingRecoveryStatusActive, recovery.Status)
	assert.Equal(t, BillingRecoveryStateTaskPending, recovery.ConsumptionState)
	won, err = PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
	require.NoError(t, err)
	assert.False(t, won)
	if fixture.tokenRequired {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("mj_token_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" {
				tx.AddError(assert.AnError)
			}
		}))
		if release {
			require.ErrorIs(t, Release(requestID), assert.AnError)
		} else {
			require.ErrorIs(t, Settle(requestID, actual), assert.AnError)
		}
		require.NoError(t, db.Callback().Update().Remove("mj_token_failure"))
		assertTaskAccountProjection(t, db, fixture.user.Id, fixture.token.Id, 70, 30, 0)
	}
	if release {
		require.NoError(t, Release(requestID))
	} else {
		require.NoError(t, Settle(requestID, actual))
	}
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("mj_statistics_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "quota_data" {
			tx.AddError(assert.AnError)
		}
	}))
	require.ErrorIs(t, ApplyBillingRecoveryConsumption(requestID), assert.AnError)
	require.NoError(t, db.Callback().Create().Remove("mj_statistics_failure"))
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.EqualValues(t, 30, persisted.Quota, "task quota must roll back with statistics")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("mj_log_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			tx.AddError(assert.AnError)
		}
	}))
	require.ErrorIs(t, ApplyBillingRecoveryConsumption(requestID), assert.AnError)
	require.NoError(t, db.Callback().Create().Remove("mj_log_failure"))
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.Equal(t, actual, persisted.Quota)
	for range 2 {
		completeTaskRecovery(t, requestID, actual, release)
	}
	assertTaskAccountProjection(t, db, fixture.user.Id, 0, 100-actual, 0, actual)
	require.NoError(t, db.First(&fixture.user, fixture.user.Id).Error)
	expectedCount := 1
	if release {
		expectedCount = 0
	}
	assert.Equal(t, expectedCount, fixture.user.RequestCount)
	require.NoError(t, db.First(&fixture.token, fixture.token.Id).Error)
	expectedTokenQuota := common.Quota(0)
	if fixture.tokenRequired {
		expectedTokenQuota = actual
	}
	assert.Equal(t, 100-expectedTokenQuota, fixture.token.RemainQuota)
	assert.Equal(t, expectedTokenQuota, fixture.token.UsedQuota)
	assert.Zero(t, fixture.token.ReservedQuota)
	require.NoError(t, db.First(&fixture.channel, fixture.channel.Id).Error)
	assert.Equal(t, actual, fixture.channel.UsedQuota)
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.Equal(t, actual, persisted.Quota)
	assert.Equal(t, task.Status, persisted.Status)
	assert.Equal(t, task.ImageUrl, persisted.ImageUrl)
	assert.Equal(t, task.Buttons, persisted.Buttons)
	assert.Equal(t, task.Properties, persisted.Properties)
	assert.Equal(t, fixture.task.PrivateData, persisted.PrivateData)
	var logs []Log
	require.NoError(t, db.Where("request_id = ?", requestID).Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, actual, logs[0].Quota)
	expectedType := LogTypeConsume
	if release {
		expectedType = LogTypeRefund
	}
	assert.Equal(t, expectedType, logs[0].Type)
	var bucket QuotaData
	require.NoError(t, db.Where("user_id = ?", fixture.user.Id).First(&bucket).Error)
	assert.Equal(t, actual, bucket.Quota)
	assert.Equal(t, expectedCount, bucket.Count, "exported request count matches committed usage statistics")
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Where("request_id = ?", requestID).Count(&count).Error)
	assert.EqualValues(t, 2, count)
}

func testMidjourneyConcurrentCompletion(t *testing.T, fixture midjourneyCompletionFixture) {
	t.Helper()
	type result struct {
		won    bool
		err    error
		status string
	}
	results, start := make(chan result, 2), make(chan struct{})
	for _, status := range []string{"SUCCESS", "FAILURE"} {
		go func(status string) {
			task := fixture.task
			task.Status, task.Progress = status, "100%"
			<-start
			won, err := PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
			results <- result{won, err, status}
		}(status)
	}
	close(start)
	winner := ""
	for range 2 {
		result := <-results
		require.NoError(t, result.err)
		if result.won {
			assert.Empty(t, winner, "only one terminal transition may win")
			winner = result.status
		}
	}
	require.NotEmpty(t, winner)
	var persisted Midjourney
	require.NoError(t, fixture.db.First(&persisted, fixture.task.Id).Error)
	assert.Equal(t, winner, persisted.Status)
	requestID := fixture.task.PrivateData.SubmissionRequestID
	var recoveries []BillingRecovery
	require.NoError(t, fixture.db.Where("request_id = ?", requestID).Find(&recoveries).Error)
	require.Len(t, recoveries, 1)
	actual := common.Quota(30)
	if winner == "FAILURE" {
		actual = 0
	}
	assert.Equal(t, actual-30, recoveries[0].SettlementDelta)
	for range 2 {
		completeTaskRecovery(t, requestID, actual, winner == "FAILURE")
	}
	assertTaskAccountProjection(t, fixture.db, fixture.user.Id, fixture.token.Id, 100-actual, 0, actual)
	var count int64
	require.NoError(t, fixture.db.Model(&BillingEvent{}).Where("request_id = ?", requestID).Count(&count).Error)
	assert.EqualValues(t, 2, count)
}
