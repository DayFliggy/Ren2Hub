package model

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
			require.NoError(t, db.AutoMigrate(&Midjourney{}, &User{}, &Token{}, &Channel{}, &UserSubscription{}, &Log{}, &QuotaData{}, &BillingRecovery{}, &BillingRecoveryAdjustment{}))
			t.Run("accepted-submission", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "accepted", "wallet", false, true)
				testMidjourneyAcceptedSubmission(t, fixture)
			})
			t.Run("concurrent-owner", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "owner", "wallet", false, true)
				testMidjourneyConcurrentOwner(t, fixture)
			})
			for _, source := range []string{"wallet", "subscription"} {
				for _, status := range []string{"SUCCESS", "FAILURE"} {
					t.Run(source+"/"+status, func(t *testing.T) {
						fixture := newMidjourneyCompletionFixture(t, db, source+status, source, false, true)
						testMidjourneyCompletionRecovery(t, fixture, status)
					})
				}
			}
			for _, test := range []struct {
				name          string
				legacy        bool
				tokenRequired bool
			}{{"legacy-token", true, true}, {"legacy-wallet-only", true, false}, {"playground", false, false}} {
				t.Run(test.name, func(t *testing.T) {
					fixture := newMidjourneyCompletionFixture(t, db, test.name, "wallet", test.legacy, test.tokenRequired)
					testMidjourneyCompletionRecovery(t, fixture, "FAILURE")
				})
			}
			t.Run("concurrent-terminal", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "concurrent", "wallet", false, true)
				testMidjourneyConcurrentCompletion(t, fixture)
			})
			t.Run("submission-pending", func(t *testing.T) {
				fixture := newMidjourneyCompletionFixture(t, db, "pending", "wallet", false, true)
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

func newMidjourneySubmission(t *testing.T, fixture midjourneyCompletionFixture, requestID string) (Midjourney, BillingConsumption) {
	t.Helper()
	task := fixture.task
	task.Id, task.MjId, task.Status = 0, "accepted-provider-id", "SUBMITTED"
	task.PrivateData.SubmissionRequestID, task.PrivateData.CredentialVersion = requestID, 1
	require.NoError(t, fixture.db.Create(&BillingRecovery{
		RequestID: requestID, UserID: task.UserId, TokenID: task.PrivateData.TokenId, TokenRequired: true,
		Source: "wallet", Status: BillingRecoveryStatusActive, FundingAmount: int64(task.Quota), TokenAmount: int64(task.Quota),
		PreConsumeState: BillingRecoveryPreConsumeCommitted,
	}).Error)
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
	assert.Equal(t, BillingRecoveryStatusSettlementPending, recovery.Status)
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
	subscription  UserSubscription
	source        string
	tokenRequired bool
}

func newMidjourneyCompletionFixture(t *testing.T, db *gorm.DB, identity, source string, legacy, tokenRequired bool) midjourneyCompletionFixture {
	t.Helper()
	fixture := midjourneyCompletionFixture{db: db, source: source, tokenRequired: tokenRequired}
	fixture.user = User{Username: "mj-" + identity, AffCode: "mj-" + identity, Quota: 70, UsedQuota: 30, RequestCount: 1}
	require.NoError(t, db.Create(&fixture.user).Error)
	fixture.token = Token{UserId: fixture.user.Id, Key: "mj-" + identity, RemainQuota: 100}
	if tokenRequired {
		fixture.token.RemainQuota, fixture.token.UsedQuota = 70, 30
	}
	require.NoError(t, db.Create(&fixture.token).Error)
	fixture.channel = Channel{Name: "mj-" + identity, UsedQuota: 30}
	require.NoError(t, db.Create(&fixture.channel).Error)
	if source == "subscription" {
		fixture.subscription = UserSubscription{UserId: fixture.user.Id, AmountTotal: 1000, AmountUsed: 30}
		require.NoError(t, db.Create(&fixture.subscription).Error)
	}
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
		fixture.task.PrivateData = TaskPrivateData{TokenId: fixture.token.Id, BillingSource: source, SubscriptionId: fixture.subscription.Id,
			SubmissionRequestID: "mj-submission-" + identity, CredentialID: "local-credential", EndpointID: "local-endpoint"}
		submission := BillingRecovery{RequestID: fixture.task.PrivateData.SubmissionRequestID, UserID: fixture.user.Id, TokenID: fixture.token.Id,
			TokenRequired: tokenRequired, Source: source, SubscriptionID: fixture.subscription.Id,
			Status: BillingRecoveryStatusSettled, ConsumptionState: BillingRecoveryStateCompleted}
		require.NoError(t, db.Create(&submission).Error)
	}
	require.NoError(t, db.Create(&fixture.task).Error)
	fixture.consumption = BillingConsumption{Log: Log{UserId: fixture.user.Id, TokenId: logTokenID, ChannelId: fixture.channel.Id,
		Username: fixture.user.Username, ModelName: "mj_imagine", CreatedAt: 7200}, CountUsage: true, LogEnabled: true, ExportEnabled: true}
	return fixture
}

func testMidjourneyCompletionRecovery(t *testing.T, fixture midjourneyCompletionFixture, status string) {
	t.Helper()
	db, task := fixture.db, fixture.task
	task.Status, task.Progress, task.FinishTime = status, "100%", 7300
	task.ImageUrl = "https://result.example.invalid/mj"
	task.Buttons, task.Properties = `[{"label":"V1"}]`, `{"seed":"123"}`
	// Completion updates provider fields without overwriting durable ownership.
	task.PrivateData.CredentialID = "must-not-replace-original"
	actual := 30
	if status == "FAILURE" {
		actual = 0
		task.FailReason = "provider confirmed task failure"
	}
	requestID := MidjourneyBillingRequestID(task.Id)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("mj_completion_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			tx.AddError(assert.AnError)
		}
	}))
	won, err := PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
	require.Error(t, err)
	assert.False(t, won)
	require.NoError(t, db.Callback().Update().Remove("mj_completion_failure"))
	var persisted Midjourney
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.Equal(t, "IN_PROGRESS", persisted.Status)
	var count int64
	require.NoError(t, db.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Count(&count).Error)
	assert.Zero(t, count, "intent must roll back with the failed terminal write")
	won, err = PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
	require.NoError(t, err)
	assert.True(t, won)
	won, err = PersistMidjourneyCompletion(context.Background(), &task, "IN_PROGRESS", fixture.consumption)
	require.NoError(t, err)
	assert.False(t, won)
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
	if actual == 0 && fixture.tokenRequired {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("mj_token_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "tokens" {
				tx.AddError(assert.AnError)
			}
		}))
		require.Error(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
		require.NoError(t, db.Callback().Update().Remove("mj_token_failure"))
	}
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
	require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
	require.NoError(t, MarkBillingRecoverySettled(requestID))
	if actual == 0 {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("mj_statistics_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "channels" {
				tx.AddError(assert.AnError)
			}
		}))
		require.Error(t, ApplyBillingRecoveryConsumption(requestID))
		require.NoError(t, db.Callback().Update().Remove("mj_statistics_failure"))
		require.NoError(t, db.First(&persisted, task.Id).Error)
		assert.Equal(t, 30, persisted.Quota, "task quota must roll back with statistics")
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("mj_log_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "logs" {
				tx.AddError(assert.AnError)
			}
		}))
		require.Error(t, ApplyBillingRecoveryConsumption(requestID))
		require.NoError(t, db.Callback().Create().Remove("mj_log_failure"))
		require.NoError(t, db.First(&persisted, task.Id).Error)
		assert.Zero(t, persisted.Quota)
	}
	for range 2 {
		require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
		require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
		require.NoError(t, ApplyBillingRecoveryConsumption(requestID))
	}
	require.NoError(t, db.First(&fixture.user, fixture.user.Id).Error)
	if fixture.source == "wallet" {
		assert.Equal(t, 100-actual, fixture.user.Quota)
	} else {
		require.NoError(t, db.First(&fixture.subscription, fixture.subscription.Id).Error)
		assert.EqualValues(t, actual, fixture.subscription.AmountUsed)
		assert.Equal(t, 70, fixture.user.Quota)
	}
	assert.Equal(t, actual, fixture.user.UsedQuota)
	assert.Equal(t, 1, fixture.user.RequestCount, "terminal adjustment never counts another submission")
	require.NoError(t, db.First(&fixture.token, fixture.token.Id).Error)
	expectedTokenQuota := 0
	if fixture.tokenRequired {
		expectedTokenQuota = actual
	}
	assert.Equal(t, 100-expectedTokenQuota, fixture.token.RemainQuota)
	assert.Equal(t, expectedTokenQuota, fixture.token.UsedQuota)
	require.NoError(t, db.First(&fixture.channel, fixture.channel.Id).Error)
	assert.EqualValues(t, actual, fixture.channel.UsedQuota)
	if fixture.task.ChannelId != fixture.channel.Id {
		var routeChannel Channel
		require.NoError(t, db.First(&routeChannel, fixture.task.ChannelId).Error)
		assert.Zero(t, routeChannel.UsedQuota)
	}
	require.NoError(t, db.First(&persisted, task.Id).Error)
	assert.Equal(t, actual, persisted.Quota)
	assert.Equal(t, task.Status, persisted.Status)
	assert.Equal(t, task.ImageUrl, persisted.ImageUrl)
	assert.Equal(t, task.Buttons, persisted.Buttons)
	assert.Equal(t, task.Properties, persisted.Properties)
	assert.Equal(t, fixture.task.PrivateData, persisted.PrivateData)
	var logs []Log
	require.NoError(t, db.Where("request_id = ?", requestID).Find(&logs).Error)
	if actual == 30 {
		assert.Empty(t, logs)
	} else {
		require.Len(t, logs, 1)
		assert.Equal(t, LogTypeRefund, logs[0].Type)
		assert.Equal(t, 30, logs[0].Quota)
		var bucket QuotaData
		require.NoError(t, db.Where("user_id = ?", fixture.user.Id).First(&bucket).Error)
		assert.Zero(t, bucket.Count)
		assert.Equal(t, -30, bucket.Quota)
	}
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
			results <- result{won: won, err: err, status: status}
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
	requestID := MidjourneyBillingRequestID(fixture.task.Id)
	var recoveries []BillingRecovery
	require.NoError(t, fixture.db.Where("request_id = ?", requestID).Find(&recoveries).Error)
	require.Len(t, recoveries, 1)
	actual := 30
	if winner == "FAILURE" {
		actual = 0
	}
	assert.EqualValues(t, actual-30, recoveries[0].SettlementDelta)
	for range 2 {
		require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentFunding, BillingRecoveryOperationSettle))
		require.NoError(t, ApplyBillingRecoveryAdjustment(requestID, BillingRecoveryComponentToken, BillingRecoveryOperationSettle))
		require.NoError(t, MarkBillingRecoverySettled(requestID))
		require.NoError(t, ApplyBillingRecoveryConsumption(requestID))
	}
	require.NoError(t, fixture.db.First(&fixture.user, fixture.user.Id).Error)
	assert.Equal(t, 100-actual, fixture.user.Quota, fmt.Sprintf("winner %s", winner))
	assert.Equal(t, actual, fixture.user.UsedQuota)
	assert.Equal(t, 1, fixture.user.RequestCount)
}
