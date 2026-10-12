package service

import (
	"context"
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"os"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if err := os.Setenv("CRYPTO_SECRET", "service-test-persistent-evidence-key"); err != nil {
		panic(err)
	}
	common.CryptoSecret = "service-test-persistent-evidence-key"
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic("failed to open test db: " + err.Error())
	}
	sqlDB, err := db.DB()
	if err != nil {
		panic("failed to get sql.DB: " + err.Error())
	}
	sqlDB.SetMaxOpenConns(1)

	model.DB = db
	model.LOG_DB = db

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.LogConsumeEnabled = true

	if err := db.AutoMigrate(
		&model.Task{},
		&model.Option{}, &model.Ability{}, &model.Model{}, &model.Vendor{},
		&model.TaskResultEvent{}, &model.PerfMetric{},
		&model.User{},
		&model.Token{},
		&model.Log{},
		&model.QuotaData{},
		&model.Channel{},
		&model.Midjourney{},
		&model.TopUp{},
		&model.BillingRecovery{},
		&model.BillingEvent{},
		&model.BillingAttempt{}, &model.BillingPriceVersion{},
		&model.SystemTask{},
		&model.SystemTaskLock{},
	); err != nil {
		panic("failed to migrate: " + err.Error())
	}

	os.Exit(m.Run())
}

func truncate(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		model.DB.Exec("DELETE FROM tasks")
		model.DB.Exec("DELETE FROM task_result_events")
		model.DB.Exec("DELETE FROM perf_metrics")
		model.DB.Exec("DELETE FROM users")
		model.DB.Exec("DELETE FROM tokens")
		model.DB.Exec("DELETE FROM logs")
		model.DB.Exec("DELETE FROM quota_data")
		model.DB.Exec("DELETE FROM channels")
		model.DB.Exec("DELETE FROM midjourneys")
		model.DB.Exec("DELETE FROM top_ups")
		model.DB.Exec("DELETE FROM billing_events")
		model.DB.Exec("DELETE FROM billing_recoveries")
		model.DB.Exec("DELETE FROM billing_attempts")
		model.DB.Exec("DELETE FROM system_task_locks")
		model.DB.Exec("DELETE FROM system_tasks")
	})
}

func seedUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &model.User{Id: id, Username: "test_user", Quota: common.Quota(quota), Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(user).Error)
}

func seedToken(t *testing.T, id int, userId int, key string, remainQuota int) {
	t.Helper()
	token := &model.Token{
		Id:          id,
		UserId:      userId,
		Key:         key,
		Name:        "test_token",
		Status:      common.TokenStatusEnabled,
		RemainQuota: common.Quota(remainQuota),
		UsedQuota:   0,
	}
	require.NoError(t, model.DB.Create(token).Error)
}

func seedChannel(t *testing.T, id int) {
	t.Helper()
	ch := &model.Channel{Id: id, Name: "test_channel", Key: "sk-test", Status: common.ChannelStatusEnabled}
	require.NoError(t, model.DB.Create(ch).Error)
}

func markBillingRecoveryStale(t *testing.T, requestID string) {
	t.Helper()
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).
		Where("request_id = ?", requestID).
		Update("updated_at", common.GetTimestamp()-3600).Error)
}

func makeTask(userId, channelId, quota, tokenId int, billingSource string) *model.Task {
	return &model.Task{
		TaskID:    "task_" + time.Now().Format("150405.000"),
		UserId:    userId,
		ChannelId: channelId,
		Quota:     common.Quota(quota),
		Status:    model.TaskStatus(model.TaskStatusInProgress),
		Group:     "default",
		Data:      json.RawMessage(`{}`),
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		Properties: model.Properties{
			OriginModelName: "test-model",
		},
		PrivateData: model.TaskPrivateData{
			BillingSource: billingSource,
			TokenId:       tokenId,
			BillingContext: &model.TaskBillingContext{
				OriginModelName: "test-model",
			},
		},
	}
}

func getUserQuota(t *testing.T, id int) int {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("available_quota").Where("id = ?", id).First(&user).Error)
	return int(user.Quota)
}

func getUserUsageAccounting(t *testing.T, id int) (int, int) {
	t.Helper()
	var user model.User
	require.NoError(t, model.DB.Select("used_quota", "request_count").Where("id = ?", id).First(&user).Error)
	return int(user.UsedQuota), user.RequestCount
}

func getChannelUsedQuota(t *testing.T, id int) int64 {
	t.Helper()
	var channel model.Channel
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&channel).Error)
	return int64(channel.UsedQuota)
}

func getTokenRemainQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("available_quota").Where("id = ?", id).First(&token).Error)
	return int(token.RemainQuota)
}

func getTokenUsedQuota(t *testing.T, id int) int {
	t.Helper()
	var token model.Token
	require.NoError(t, model.DB.Select("used_quota").Where("id = ?", id).First(&token).Error)
	return int(token.UsedQuota)
}

func getTaskQuota(t *testing.T, id int64) int {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.Select("quota").Where("id = ?", id).First(&task).Error)
	return int(task.Quota)
}

func getLastLog(t *testing.T) *model.Log {
	t.Helper()
	var log model.Log
	err := model.LOG_DB.Order("id desc").First(&log).Error
	if err != nil {
		return nil
	}
	return &log
}

func countLogs(t *testing.T) int64 {
	t.Helper()
	var count int64
	model.LOG_DB.Model(&model.Log{}).Count(&count)
	return count
}

// completeTestTask finishes a task: a completed one logs reason as how it was
// priced, a failed one logs reason as its fail reason.
func completeTestTask(t *testing.T, ctx context.Context, task *model.Task, status model.TaskStatus, actualQuota int, reason string) error {
	t.Helper()
	if task.PrivateData.SubmissionRequestID == "" {
		acceptTestTask(t, task)
	}
	fromStatus := task.Status
	task.Status, task.Progress = status, "100%"
	if status == model.TaskStatusSuccess {
		usage := billing.Usage{InputTokens: int64(actualQuota), ContextTokens: int64(actualQuota), Source: "provider", Complete: true, Terminal: "completed", Quantities: map[string]string{"calls": "1"}}
		usage, charge, err := model.EvaluateBilling(task.PrivateData.BillingContext.Quote, usage, billingexpr.RequestInput{}, task.PrivateData.SubmissionRequestID, task.PrivateData.BillingContext.AttemptID, "settlement")
		require.NoError(t, err)
		require.EqualValues(t, actualQuota, charge.FinalQuota)
		task.PrivateData.BillingContext.Usage, task.PrivateData.BillingContext.Charge = &usage, charge
	}
	var settlement *common.Message
	if status == model.TaskStatusSuccess {
		settlement = common.NewMessage(reason)
	} else {
		task.FailReason = reason
	}
	_, err := completeTaskBilling(ctx, task, fromStatus, common.Quota(actualQuota), settlement)
	return err
}

func acceptTestTask(t *testing.T, task *model.Task) {
	t.Helper()
	task.PrivateData.SubmissionRequestID = "accepted:" + task.TaskID
	if task.PrivateData.UpstreamTaskID == "" {
		task.PrivateData.UpstreamTaskID = "provider:" + task.TaskID
	}
	task.PrivateData.BillingSource = BillingSourceWallet
	if task.PrivateData.BillingContext == nil || task.PrivateData.BillingContext.Quote == nil {
		quote, err := billing.NewQuote(billingTestStoredPrice(t, "test-model", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": "p * 2"}), "1", nil, time.Now())
		require.NoError(t, err)
		task.PrivateData.BillingContext = &model.TaskBillingContext{Quote: quote, Usage: &billing.Usage{InputTokens: int64(task.Quota), Quantities: map[string]string{"calls": "1"}, Source: "accepted_parameters", Estimated: true}, OriginModelName: "test-model"}
	}
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: task.PrivateData.SubmissionRequestID, UserID: task.UserId, TokenID: task.PrivateData.TokenId, TokenRequired: task.PrivateData.TokenId > 0, Source: BillingSourceWallet})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(task.PrivateData.SubmissionRequestID, task.Quota))
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), task.PrivateData.SubmissionRequestID, task.PrivateData.SubmissionRequestID+":attempt", task.ChannelId, task.PrivateData.BillingContext.Quote))
	task.PrivateData.BillingContext.AttemptID = task.PrivateData.SubmissionRequestID + ":attempt"
	if task.PrivateData.BillingContext.Usage.Source == "" {
		// Submission pricing labels its estimate this way; the frozen fallback relies on it.
		task.PrivateData.BillingContext.Usage.Source = billing.UsageSourceRequestEstimate
	}
	input := billingexpr.RequestInput{}
	if task.PrivateData.BillingContext.Quote.PriceVersion.TaskUsage {
		var err error
		input.Usage, err = TaskExpressionUsage(task.PrivateData.BillingContext.Usage.EstimatedFacts, task.PrivateData.BillingContext.Quote.PriceVersion.UsageSchema)
		require.NoError(t, err)
	}
	estimate, charge, err := model.EvaluateBilling(task.PrivateData.BillingContext.Quote, *task.PrivateData.BillingContext.Usage, input, task.PrivateData.SubmissionRequestID, task.PrivateData.BillingContext.AttemptID, "estimate")
	require.NoError(t, err)
	require.NoError(t, model.PersistBillingEstimate(task.PrivateData.BillingContext.AttemptID, estimate, charge))
	task.PrivateData.BillingContext.Usage = &estimate
	if task.ID > 0 {
		require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
	}
	require.NoError(t, model.PersistAcceptedTask(task, task.PrivateData.SubmissionRequestID, model.BillingConsumption{AttemptID: task.PrivateData.BillingContext.AttemptID, Log: model.Log{UserId: task.UserId, TokenId: task.PrivateData.TokenId, ChannelId: task.ChannelId, Quota: task.Quota}, CountUsage: true, LogEnabled: true}))
}

func TestTaskBillingAcceptedHoldAndSingleTerminalCharge(t *testing.T) {
	for _, actual := range []int{0, 20, 40, 140} {
		t.Run(decimal.NewFromInt(int64(actual)).String(), func(t *testing.T) {
			truncate(t)
			seedUser(t, 1, 100)
			seedToken(t, 1, 1, "task", 100)
			seedChannel(t, 1)
			task := makeTask(1, 1, 40, 1, BillingSourceWallet)
			acceptTestTask(t, task)
			before := billing.ReadOperationalMetrics().Settlements[billing.SettlementConfirmed]
			require.ErrorIs(t, model.PrepareBillingAttempt(context.Background(), task.PrivateData.SubmissionRequestID,
				task.PrivateData.SubmissionRequestID+":replacement", task.ChannelId, task.PrivateData.BillingContext.Quote),
				model.ErrBillingRecoveryStateConflict, "an accepted task owns terminal accounting and cannot become a new request attempt")
			assert.Equal(t, 60, getUserQuota(t, 1))
			used, count := getUserUsageAccounting(t, 1)
			assert.Zero(t, used)
			assert.Zero(t, count)
			assert.ErrorIs(t, model.MarkBillingRecoveryRefundPending(task.PrivateData.SubmissionRequestID, nil), model.ErrBillingRecoveryStateConflict)
			status := model.TaskStatus(model.TaskStatusSuccess)
			if actual == 0 {
				status = model.TaskStatusFailure
			}
			require.NoError(t, completeTestTask(t, context.Background(), task, status, actual, "confirmed terminal"))
			require.NoError(t, completeTestTask(t, context.Background(), task, status, actual, "duplicate terminal"))
			assert.Equal(t, 100-actual, getUserQuota(t, 1))
			assert.Equal(t, 100-actual, getTokenRemainQuota(t, 1))
			assert.Equal(t, actual, getTokenUsedQuota(t, 1))
			assert.EqualValues(t, actual, getChannelUsedQuota(t, 1))
			used, count = getUserUsageAccounting(t, 1)
			assert.Equal(t, actual, used)
			if actual == 0 {
				assert.Zero(t, count)
			} else {
				assert.Equal(t, 1, count)
			}
			var r model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&r).Error)
			assert.EqualValues(t, actual, r.FundingAmount+r.SettlementDelta)
			assert.EqualValues(t, 1, countLogs(t))
			assert.EqualValues(t, actual, getLastLog(t).Quota)
			increment := uint64(0)
			if status == model.TaskStatusSuccess {
				increment = 1
			}
			assert.Equal(t, before+increment, billing.ReadOperationalMetrics().Settlements[billing.SettlementConfirmed])
		})
	}
}

func TestTaskPriceUsesFrozenUsageInsteadOfAdaptorMoney(t *testing.T) {
	truncate(t)
	seedUser(t, 1, 100)
	seedChannel(t, 1)
	task := makeTask(1, 1, 20, 0, BillingSourceWallet)
	task.PrivateData.BillingContext.Quote = billingTestCallQuote(t)
	task.PrivateData.BillingContext.Usage = &billing.Usage{Quantities: map[string]string{"calls": "1"}, Estimated: true}
	acceptTestTask(t, task)
	task.Status = model.TaskStatusSuccess
	quota, _, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{TotalTokens: 999, CompletionTokens: 500})
	require.NoError(t, err)
	assert.EqualValues(t, 20, quota)
	require.NotNil(t, task.PrivateData.BillingContext.Charge)
	// Completion that cannot be priced consumes the persisted submission estimate.
	task.PrivateData.BillingContext.Quote = nil
	quota, _, _, err = taskCompletionQuota(nil, task, nil)
	require.NoError(t, err)
	assert.EqualValues(t, 20, quota)
	assert.Equal(t, billing.UsageSourceRequestEstimate, task.PrivateData.BillingContext.Usage.Source)
}

func taskExpressionTestQuote(t *testing.T, expression string, schema *billingexpr.UsageSchema) *billing.Quote {
	t.Helper()
	version, err := billing.NewModelPriceVersion("test-model", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": expression}, true)
	require.NoError(t, err)
	version.UsageSchema = schema
	stored, err := model.StoreBillingPriceEvidence(context.Background(), version)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*stored, "1", nil, time.Now())
	require.NoError(t, err)
	return quote
}

// acceptedUsageTask persists an accepted wallet task whose attempt froze the
// submit facts and their estimate under quote, as task submission does.
func acceptedUsageTask(t *testing.T, quote *billing.Quote, submitted map[string]any, reserved int) *model.Task {
	t.Helper()
	truncate(t)
	const id, balance = 961, 100000000
	seedUser(t, id, balance)
	seedChannel(t, id)
	task := makeTask(id, id, reserved, 0, BillingSourceWallet)
	task.PrivateData.BillingContext = &model.TaskBillingContext{Quote: quote, Usage: &billing.Usage{Estimated: true, EstimatedFacts: submitted}, OriginModelName: "test-model"}
	acceptTestTask(t, task)
	task.PrivateData.PluginRef = &model.TaskPluginRef{Key: "plugin", Version: "1.0.0", SourceHash: quote.Hash}
	require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
	return task
}

// Facts outside the frozen schema never price a task: it settles at its own
// frozen estimate, labeled as such, rather than at a value made from them.
func TestPluginTaskCompletionUsesOnlyFrozenSchemaFacts(t *testing.T) {
	schema := &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{"seconds": {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Required: true, Maximum: "300"}}}
	quote := taskExpressionTestQuote(t, `tier("video", u("seconds") * 0.4)`, schema)
	const estimateQuota = common.Quota(6000000)
	for _, test := range []struct {
		name     string
		facts    map[string]any
		quota    common.Quota
		fallback bool
	}{
		{"actual ten seconds", map[string]any{"seconds": 10}, 2000000, false},
		{"confirmed zero", map[string]any{"seconds": 0}, 0, false},
		{"undeclared completion fact", map[string]any{"seconds": 10, "unexpected": 1}, estimateQuota, true},
		{"schema maximum", map[string]any{"seconds": 301}, estimateQuota, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := acceptedUsageTask(t, quote, map[string]any{"seconds": "30"}, int(estimateQuota))
			quota, reason, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{UsageFacts: test.facts})
			require.NoError(t, err)
			assert.Equal(t, test.quota, quota)
			usage := task.PrivateData.BillingContext.Usage
			assert.Equal(t, test.fallback, billing.IsFrozenEstimateSource(usage.Source))
			if test.fallback {
				assert.Equal(t, common.NewMessage("Completion usage could not be priced, settled at the submission estimate"), reason)
				return
			}
			assert.Equal(t, common.NewMessage("Settled at the frozen quote"), reason)
			assert.NotNil(t, usage.Evidence)
			assert.Equal(t, map[string]any{"seconds": "30"}, usage.EstimatedFacts)
		})
	}
}

func TestTaskTokenExpressionCannotPriceWithoutCompletionUsage(t *testing.T) {
	for _, dimension := range []string{"p", "c", "len", "cr", "cc", "cc1h", "img", "img_cr", "img_o", "ai", "ao"} {
		t.Run(dimension, func(t *testing.T) {
			quote := billingTestQuote(t, dimension+" * 2")
			estimate := &billing.Usage{InputTokens: 100, OutputTokens: 200, Estimated: true}
			task := &model.Task{Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{BillingContext: &model.TaskBillingContext{Quote: quote, Usage: estimate}}}
			_, _, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{})
			require.ErrorContains(t, err, "task token usage unavailable")
			assert.Nil(t, task.PrivateData.BillingContext.Charge)
			assert.Same(t, estimate, task.PrivateData.BillingContext.Usage)
		})
	}
}

func TestPluginTaskSchemaQuantityDoesNotRequireLegacyTokenCounters(t *testing.T) {
	for _, unit := range []string{"second", "token"} {
		t.Run(unit, func(t *testing.T) {
			expression := `u("quantity") * 0.4`
			if unit == "token" {
				expression = `u("quantity") * 400000 / 1000000`
			}
			quote := taskExpressionTestQuote(t, expression, &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{"quantity": {Type: "number", Unit: unit, Source: billingexpr.UsageCompletionFact, Required: true}}})
			task := acceptedUsageTask(t, quote, map[string]any{"quantity": "10"}, 2000000)
			quota, _, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{UsageFacts: map[string]any{"quantity": 10}})
			require.NoError(t, err)
			assert.EqualValues(t, 2000000, quota)
			assert.Equal(t, "4", task.PrivateData.BillingContext.Charge.TotalUSD)
		})
	}
}

func TestExplicitFreePluginDoesNotSuppressFailedUsageHooks(t *testing.T) {
	version := billingTestStoredPrice(t, "test-model", billing.PricingValues{"ModelPrice": "0"})
	quote, err := billing.NewQuote(version, "1", nil, time.Now())
	require.NoError(t, err)
	for _, hookError := range []string{"", "plugin completion usage facts are invalid"} {
		t.Run(hookError, func(t *testing.T) {
			task := &model.Task{Status: model.TaskStatusSuccess, PrivateData: model.TaskPrivateData{SubmissionRequestID: "free-task", PluginRef: &model.TaskPluginRef{Key: "plugin", Version: "1", SourceHash: "fixed"}, BillingContext: &model.TaskBillingContext{AttemptID: "free-attempt", Quote: quote, Usage: &billing.Usage{Estimated: true}}}}
			facts := map[string]any{"seconds": 10}
			quota, _, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{UsageFacts: facts, UsageError: hookError})
			if hookError != "" {
				require.Error(t, err)
				assert.Nil(t, task.PrivateData.BillingContext.Charge)
				return
			}
			require.NoError(t, err)
			assert.Zero(t, quota)
			assert.Empty(t, task.PrivateData.BillingContext.Usage.ActualFacts)
			assert.Equal(t, map[string]any{"seconds": 10}, facts)
		})
	}
}

// mergedFactsQuote prices seconds by resolution tier and halves silent output.
// The submit facts (10 s, 720p, audio) reserve 3 USD.
func mergedFactsQuote(t *testing.T) (*billing.Quote, map[string]any) {
	t.Helper()
	schema := &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"seconds":    {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Maximum: "300"},
		"resolution": {Type: "string", Enum: []string{"480p", "720p"}, Source: billingexpr.UsageCompletionFact},
		"audio":      {Type: "boolean", Source: billingexpr.UsageCompletionFact},
	}}
	quote := taskExpressionTestQuote(t, `u("seconds") * (u("resolution") == "720p" ? 0.3 : 0.1) * (u("audio") == false ? 0.5 : 1)`, schema)
	return quote, map[string]any{"seconds": "10", "resolution": "720p", "audio": true}
}

// As upstream EvaluateTaskCompletionUsage, each reported completion fact,
// explicit zero and false included, replaces its submit fact; unreported
// facts keep the frozen submit value instead of reading as missing.
func TestTaskCompletionMergesCompletionFactsOverFrozenSubmitFacts(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	for _, test := range []struct {
		name  string
		facts map[string]any
		quota common.Quota
	}{
		{"actual seconds keep submitted tier and audio", map[string]any{"seconds": "4"}, 600000},
		{"explicit false overrides submitted audio", map[string]any{"audio": false}, 750000},
		{"explicit zero overrides submitted seconds", map[string]any{"seconds": "0"}, 0},
		{"actual resolution overrides submitted tier", map[string]any{"resolution": "480p"}, 500000},
		{"every reported fact overrides", map[string]any{"seconds": "6", "resolution": "480p", "audio": false}, 150000},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := acceptedUsageTask(t, quote, submitted, 1500000)
			quota, reason, _, err := taskCompletionQuota(nil, task, &relaycommon.TaskInfo{UsageFacts: test.facts})
			require.NoError(t, err)
			assert.Equal(t, test.quota, quota)
			assert.Equal(t, common.NewMessage("Settled at the frozen quote"), reason)
			usage := task.PrivateData.BillingContext.Usage
			assert.Equal(t, "task_completed", usage.Source)
			assert.Equal(t, submitted, usage.EstimatedFacts)
			assert.Equal(t, test.facts, usage.ActualFacts)
			assert.NotNil(t, usage.Evidence)
		})
	}
}

// Merging never makes unusable completion evidence priceable: a hook error
// settles at the frozen estimate even when the hook also reported facts.
func TestUnpricedTaskCompletionStillSettlesFrozenEstimate(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	task := acceptedUsageTask(t, quote, submitted, 1500000)
	result := &relaycommon.TaskInfo{UsageFacts: map[string]any{"seconds": "4"}, UsageError: "plugin completion usage facts are invalid"}
	quota, reason, _, err := taskCompletionQuota(nil, task, result)
	require.NoError(t, err)
	assert.EqualValues(t, 1500000, quota)
	assert.Equal(t, common.NewMessage("Completion usage could not be priced, settled at the submission estimate"), reason)
	assert.True(t, billing.IsFrozenEstimateSource(task.PrivateData.BillingContext.Usage.Source))
}

// As upstream EvaluateTaskCompletionUsage, an empty completion report (no
// hook, null or {}) prices the frozen submit facts. A price that only compares
// category facts would otherwise read their defaults without failing and
// settle a task submitted at 1080p with audio at the 720p silent price.
func TestEmptyTaskCompletionPricesFrozenSubmitFacts(t *testing.T) {
	schema := &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"resolution": {Type: "string", Enum: []string{"720p", "1080p"}, Source: billingexpr.UsageCompletionFact},
		"audio":      {Type: "boolean", Source: billingexpr.UsageCompletionFact},
	}}
	quote := taskExpressionTestQuote(t, `(u("resolution") == "1080p" ? 0.5 : 0.2) * (u("audio") == true ? 2 : 1)`, schema)
	submitted := map[string]any{"resolution": "1080p", "audio": true}
	for _, test := range []struct {
		name   string
		result *relaycommon.TaskInfo
		quota  common.Quota
	}{
		{"no completion hook or null facts", &relaycommon.TaskInfo{}, 500000},
		{"empty completion facts", &relaycommon.TaskInfo{UsageFacts: map[string]any{}}, 500000},
		{"completion fact overrides submit fact", &relaycommon.TaskInfo{UsageFacts: map[string]any{"resolution": "720p"}}, 200000},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := acceptedUsageTask(t, quote, submitted, 500000)
			quota, reason, _, err := taskCompletionQuota(nil, task, test.result)
			require.NoError(t, err)
			assert.Equal(t, test.quota, quota)
			assert.Equal(t, common.NewMessage("Settled at the frozen quote"), reason)
			usage := task.PrivateData.BillingContext.Usage
			assert.Equal(t, "task_completed", usage.Source)
			assert.Equal(t, submitted, usage.EstimatedFacts)
			assert.NotNil(t, usage.Evidence)
		})
	}
}

// Submit facts are trusted only through the attempt estimate's own evidence.
// Altered facts stop settlement explicitly: the task holds the result and its
// reservation instead of settling at an estimate derived from the same row.
func TestTamperedSubmitFactsStopTaskSettlement(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	task := acceptedUsageTask(t, quote, submitted, 1500000)
	tamperAttemptEstimate(t, task)

	err := CompleteImmediateTask(context.Background(), task, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: map[string]any{"audio": false}})
	require.NoError(t, err, "the held result is durable")
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.NotEqual(t, model.TaskStatusSuccess, persisted.Status)
	assert.Equal(t, model.TaskSettlementBlockedEvidence, persisted.SettlementBlock)
	var recovery model.BillingRecovery
	require.NoError(t, model.DB.First(&recovery, "request_id = ?", task.PrivateData.SubmissionRequestID).Error)
	assert.Equal(t, model.BillingRecoveryStatusActive, recovery.Status)
	assert.Equal(t, 100000000, getUserQuota(t, task.UserId)+int(task.Quota), "only the reservation is held")
}

// A repeated completion rebuilds the same merged usage from the immutable
// attempt, so recovery replays the first evaluation and charges once.
func TestRepeatedMergedTaskCompletionChargesOnce(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	task := acceptedUsageTask(t, quote, submitted, 1500000)
	result := &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: map[string]any{"audio": false}}
	for range 2 {
		require.NoError(t, CompleteImmediateTask(context.Background(), task, result))
	}
	var persisted model.Task
	require.NoError(t, model.DB.First(&persisted, task.ID).Error)
	assert.EqualValues(t, 750000, persisted.Quota)
	usage := persisted.PrivateData.BillingContext.Usage
	assert.Equal(t, submitted, usage.EstimatedFacts)
	assert.Equal(t, map[string]any{"audio": false}, usage.ActualFacts)
	assert.Equal(t, 100000000-750000, getUserQuota(t, task.UserId))
	var count int64
	require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", task.PrivateData.SubmissionRequestID, "settle").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestTaskCompletionUsageBoundariesPreserveFundsAndTerminalEvidence(t *testing.T) {
	imageCacheEstimate := int64(15)
	for _, test := range []struct {
		name             string
		facts            map[string]any
		reserved, actual int
		taskUsage        bool
		expression       string
		estimate         billing.Usage
	}{
		{name: "schema quantity retains its unit", facts: map[string]any{"seconds": 10}, reserved: 6000000, actual: 2000000, taskUsage: true, expression: `u("seconds") * 0.4`},
		{name: "free keeps provider facts", facts: map[string]any{"seconds": 10}},
		{name: "missing image cache usage settles at submission estimate", reserved: 30, actual: 15, expression: "img_cr * 2",
			estimate: billing.Usage{InputTokens: 15, ContextTokens: 15, InputIncludesCache: true, ImageInputTokens: 15, CacheReadTokens: 15, ImageCacheReadTokens: &imageCacheEstimate}},
	} {
		t.Run(test.name, func(t *testing.T) {
			truncate(t)
			const id, balance = 953, 10000000
			seedUser(t, id, balance)
			seedToken(t, id, id, "task-usage-boundaries", balance)
			seedChannel(t, id)
			task := makeTask(id, id, test.reserved, id, BillingSourceWallet)
			var quote *billing.Quote
			if test.taskUsage {
				quote = taskExpressionTestQuote(t, test.expression, &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{"seconds": {Type: "number", Unit: "second", Source: billingexpr.UsageCompletionFact, Required: true}}})
			} else if test.expression != "" {
				quote = billingTestQuote(t, test.expression)
			} else {
				version := billingTestStoredPrice(t, "test-model", billing.PricingValues{"ModelPrice": "0"})
				var err error
				quote, err = billing.NewQuote(version, "1", nil, time.Now())
				require.NoError(t, err)
			}
			estimate := test.estimate
			estimate.Estimated, estimate.EstimatedFacts = true, map[string]any{"seconds": 30}
			task.PrivateData.BillingContext = &model.TaskBillingContext{Quote: quote, Usage: &estimate}
			acceptTestTask(t, task)
			task.PrivateData.PluginRef = &model.TaskPluginRef{Key: "usage-boundaries", Version: "1.0.0", SourceHash: quote.Hash}
			require.NoError(t, model.DB.Model(task).Update("private_data", task.PrivateData).Error)
			for range 2 {
				require.NoError(t, CompleteImmediateTask(context.Background(), task, &relaycommon.TaskInfo{Status: model.TaskStatusSuccess, UsageFacts: test.facts}))
			}
			var persisted model.Task
			require.NoError(t, model.DB.First(&persisted, task.ID).Error)
			assert.EqualValues(t, model.TaskStatusSuccess, persisted.Status)
			require.NotNil(t, persisted.PrivateData.CompletionUsage)
			wantFacts, err := common.Marshal(test.facts)
			require.NoError(t, err)
			gotFacts, err := common.Marshal(persisted.PrivateData.CompletionUsage.Facts)
			require.NoError(t, err)
			assert.JSONEq(t, string(wantFacts), string(gotFacts))
			var recovery model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", task.PrivateData.SubmissionRequestID).First(&recovery).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			assert.EqualValues(t, test.actual, persisted.Quota)
			assert.Equal(t, balance-test.actual, getUserQuota(t, id))
			assert.Equal(t, balance-test.actual, getTokenRemainQuota(t, id))
			assert.Equal(t, test.actual, getTokenUsedQuota(t, id))
			var count int64
			require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", task.PrivateData.SubmissionRequestID, "settle").Count(&count).Error)
			assert.EqualValues(t, 1, count)
			require.NoError(t, model.DB.Model(&model.TaskResultEvent{}).Where("task_id = ?", task.ID).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			var metric model.PerfMetric
			require.NoError(t, model.DB.First(&metric).Error)
			assert.EqualValues(t, 1, metric.RequestCount)
			assert.EqualValues(t, 1, metric.SuccessCount)
		})
	}
}

// A completed task's consumption log stores how it was priced as
// content_parts, which the web console renders in the viewer's language;
// content keeps the English text. A failed task's log carries its fail reason
// as written, with no content_parts.
func TestTaskCompletionLogContent(t *testing.T) {
	quote, submitted := mergedFactsQuote(t)
	for _, test := range []struct {
		name    string
		result  *relaycommon.TaskInfo
		content string
	}{
		{"priced", &relaycommon.TaskInfo{UsageFacts: map[string]any{"seconds": "4"}}, "Settled at the frozen quote"},
		{"unpriced", &relaycommon.TaskInfo{UsageError: "plugin completion usage hook failed"}, "Completion usage could not be priced, settled at the submission estimate"},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := acceptedUsageTask(t, quote, submitted, 1500000)
			from := task.Status
			task.Status, task.Progress = model.TaskStatusSuccess, "100%"
			require.NoError(t, settleTaskResult(context.Background(), task, from, test.result))
			require.NoError(t, model.ApplyBillingRecoveryConsumption(task.PrivateData.SubmissionRequestID))

			log := getLastLog(t)
			require.NotNil(t, log)
			assert.Equal(t, test.content, log.Content)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
			assert.Equal(t, []any{map[string]any{"key": test.content}}, other["content_parts"])
			assert.Equal(t, true, other["is_task"], "task settlement logs select task billing presentation")
			assert.Equal(t, test.content, other["reason"])
		})
	}

	t.Run("failed", func(t *testing.T) {
		task := acceptedUsageTask(t, quote, submitted, 1500000)
		require.NoError(t, failTaskWithoutResult(context.Background(), task, "upstream said no"))
		require.NoError(t, model.ApplyBillingRecoveryConsumption(task.PrivateData.SubmissionRequestID))
		log := getLastLog(t)
		require.NotNil(t, log)
		assert.Equal(t, "upstream said no", log.Content)
		var other map[string]any
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		assert.NotContains(t, other, "content_parts")
		assert.Equal(t, true, other["is_task"], "released task logs retain their task identity")
		assert.Equal(t, "upstream said no", other["reason"])
	})
}
