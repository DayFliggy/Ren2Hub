package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const pluginExitRequestID = "plugin-process-exit"

// The provider stays in the parent process, so abrupt child exits cannot erase
// the accepted-send count. Each recovery runs in a fresh process and database
// connection; no application shutdown handler or deferred rollback can run.
func TestTaskPluginProcessExitRecovery(t *testing.T) {
	for _, stage := range []string{"accepted_before_task", "terminal_metrics"} {
		t.Run(stage, func(t *testing.T) {
			var sends atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/submit" {
					http.Error(w, "unexpected provider request", http.StatusBadRequest)
					return
				}
				var body map[string]string
				if err := common.DecodeJson(r.Body, &body); err != nil || body["version"] != "1.0.0" {
					http.Error(w, "wrong executable version", http.StatusBadRequest)
					return
				}
				sends.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusAccepted)
				_, _ = w.Write([]byte(`{"id":"provider-accepted","calls":1}`))
			}))
			defer provider.Close()
			path := filepath.Join(t.TempDir(), "plugin.db")
			runTaskPluginExitWorker(t, path, stage, provider.URL, 74)
			require.EqualValues(t, 1, sends.Load(), "the provider accepted before the crash")
			db := openTaskPluginExitDatabase(t, path)
			var taskCount, metricCount int64
			require.NoError(t, db.Model(&model.Task{}).Count(&taskCount).Error)
			require.NoError(t, db.Model(&model.PerfMetric{}).Count(&metricCount).Error)
			assert.Zero(t, metricCount, "the uncommitted metric projection must not survive exit")
			if stage == "accepted_before_task" {
				assert.Zero(t, taskCount, "the accepted-task transaction was interrupted before commit")
			} else {
				assert.EqualValues(t, 1, taskCount)
				var event model.TaskResultEvent
				require.NoError(t, db.First(&event).Error)
				assert.Equal(t, model.BillingRecoveryStatePending, event.MetricsState)
			}

			// A newer active version cannot remove the old, still-referenced source.
			newVersion := taskPluginExitVersion("2.0.0")
			require.NoError(t, model.SaveTaskPlugin(&newVersion))
			require.NoError(t, model.ActivateTaskPlugin(newVersion.Key, newVersion.Version))
			for range 2 {
				runTaskPluginExitWorker(t, path, "recover", provider.URL, 0)
			}
			assert.EqualValues(t, 1, sends.Load(), "recovery must never resubmit the accepted provider job")
			assertTaskPluginExitState(t, db, stage)
		})
	}
}

func runTaskPluginExitWorker(t *testing.T, path, stage, provider string, exitCode int) {
	t.Helper()
	binary, err := os.Executable()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, binary, "-test.run=^TestTaskPluginProcessExitWorker$", "-test.timeout=8s")
	child.Env = append(os.Environ(), "TASK_PLUGIN_EXIT_STAGE="+stage, "TASK_PLUGIN_EXIT_DATABASE="+path,
		"TASK_PLUGIN_EXIT_PROVIDER="+provider)
	output, err := child.CombinedOutput()
	if exitCode == 0 {
		require.NoError(t, err, "%s", output)
		return
	}
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit, "%s", output)
	require.Equal(t, exitCode, exit.ExitCode(), "%s", output)
}

func openTaskPluginExitDatabase(t *testing.T, path string) *gorm.DB {
	t.Helper()
	db := openBillingExitDatabase(t, path)
	require.NoError(t, db.AutoMigrate(&model.TaskPlugin{}, &model.TaskPluginNamespace{}, &model.TaskPluginExecution{},
		&model.Task{}, &model.TaskResultEvent{}, &model.PerfMetric{}))
	return db
}

func taskPluginExitVersion(version string) model.TaskPlugin {
	source := fmt.Sprintf(`
export const meta = {apiVersion: 1, key: "process-exit", name: "Process Exit",
  version: %q, author: {name: "Test"}, models: ["test"], fetchMode: "per_task"};
export function buildSubmitRequest(ctx) {
  return {url: ctx.baseUrl + "/submit", method: "POST", body: {version: %q}};
}
export function parseSubmitResponse(ctx, response) { return {taskId: response.body.id}; }
export function buildQueryRequest() { throw new Error("unexpected query"); }
export function parseTaskResult() { throw new Error("unexpected poll"); }
`, version, version)
	return model.TaskPlugin{Key: "process-exit", Version: version, APIVersion: 1, Enabled: true,
		Source: model.LongText(source), SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}
}

func TestTaskPluginProcessExitWorker(t *testing.T) {
	stage := os.Getenv("TASK_PLUGIN_EXIT_STAGE")
	if stage == "" {
		t.Skip("subprocess helper")
	}
	db := openTaskPluginExitDatabase(t, os.Getenv("TASK_PLUGIN_EXIT_DATABASE"))
	ctx := context.Background()
	constant.TaskPluginEnabled = true
	version := taskPluginExitVersion("1.0.0")
	ref := model.TaskPluginRef{Key: version.Key, Version: version.Version, SourceHash: version.SourceHash}
	if stage == "recover" {
		plugin, err := LoadTaskPluginExact(ref)
		require.NoError(t, err)
		require.Equal(t, "1.0.0", plugin.Meta.Version)
		_, err = recoverOneBillingRecovery(ctx, pluginExitRequestID)
		require.NoError(t, err)
		require.NoError(t, model.RecoverTaskResultMetrics(ctx, 10))
		return
	}
	require.Contains(t, []string{"accepted_before_task", "terminal_metrics"}, stage)
	require.NoError(t, model.SaveTaskPlugin(&version))
	require.NoError(t, db.Create(&model.User{Id: 1, Username: pluginExitRequestID, Quota: 100}).Error)
	require.NoError(t, db.Create(&model.Token{Id: 1, UserId: 1, Key: "isolated-plugin-exit-token", RemainQuota: 100}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Name: pluginExitRequestID}).Error)
	_, err := model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: pluginExitRequestID,
		UserID: 1, TokenID: 1, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(pluginExitRequestID, 40))
	quote := billingTestCallQuote(t)
	require.NoError(t, model.PrepareBillingAttemptWithPlugin(ctx, pluginExitRequestID, "plugin-attempt", 1, quote, &ref))
	estimate, estimateCharge, err := model.EvaluateBilling(quote, billing.Usage{Estimated: true, Source: billing.UsageSourceRequestEstimate, Quantities: map[string]string{"calls": "1"}}, billingexpr.RequestInput{}, pluginExitRequestID, "plugin-attempt", "estimate")
	require.NoError(t, err)
	require.NoError(t, model.PersistBillingEstimate("plugin-attempt", estimate, estimateCharge))

	plugin, err := LoadTaskPluginExact(ref)
	require.NoError(t, err)
	descriptor, err := plugin.Engine.Call(ctx, "buildSubmitRequest", map[string]any{"baseUrl": os.Getenv("TASK_PLUGIN_EXIT_PROVIDER")})
	require.NoError(t, err)
	request, ok := descriptor.(map[string]any)
	require.True(t, ok)
	body, err := common.Marshal(request["body"])
	require.NoError(t, err)
	upstream, err := http.NewRequestWithContext(ctx, request["method"].(string), request["url"].(string), bytes.NewReader(body))
	require.NoError(t, err)
	upstream.Header.Set("Content-Type", "application/json")
	client := http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}}
	response, err := client.Do(upstream)
	require.NoError(t, err)
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	var accepted map[string]any
	require.NoError(t, common.DecodeJson(response.Body, &accepted))
	require.NoError(t, response.Body.Close())
	parsed, err := plugin.Engine.Call(ctx, "parseSubmitResponse", map[string]any{}, map[string]any{"body": accepted})
	require.NoError(t, err)
	result, ok := parsed.(map[string]any)
	require.True(t, ok)
	providerID, ok := result["taskId"].(string)
	require.True(t, ok)
	require.Equal(t, "provider-accepted", providerID)
	consumption := model.BillingConsumption{AttemptID: "plugin-attempt", Log: model.Log{UserId: 1, TokenId: 1, ChannelId: 1,
		Quota: 20, ModelName: "test", CreatedAt: 7200}, CountUsage: true, LogEnabled: true, ExportEnabled: true}
	task := model.Task{TaskID: "task-plugin-exit", UserId: 1, ChannelId: 1, Quota: 20, Status: model.TaskStatusInProgress,
		SubmitTime: 7200, StartTime: 7210, Properties: model.Properties{OriginModelName: "test"},
		PrivateData: model.TaskPrivateData{UpstreamTaskID: providerID, TokenId: 1, BillingSource: "wallet",
			SubmissionRequestID: pluginExitRequestID, PluginAttemptID: "plugin-attempt", PluginRef: &ref,
			BillingContext: &model.TaskBillingContext{Quote: quote}}}
	if stage == "accepted_before_task" {
		// The provider accepted and AttachTaskPluginExecution already ran inside
		// this transaction. Exit must roll back attachment and task_pending only.
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("plugin_exit_before_task", func(tx *gorm.DB) {
			if tx.Statement.Table == "tasks" {
				var execution model.TaskPluginExecution
				require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Where("attempt_id = ?", "plugin-attempt").First(&execution).Error)
				require.Equal(t, task.TaskID, execution.TaskID)
				os.Exit(74)
			}
		}))
	}
	require.NoError(t, model.PersistAcceptedTask(&task, pluginExitRequestID, consumption))
	require.Equal(t, "terminal_metrics", stage, "the requested pre-task crash was not reached")
	usage := billing.Usage{Quantities: map[string]string{"calls": fmt.Sprint(accepted["calls"])}, Source: "provider", Complete: true, Terminal: "completed"}
	usage, charge, err := model.EvaluateBilling(quote, usage, billingexpr.RequestInput{}, pluginExitRequestID, "plugin-attempt", "settlement")
	require.NoError(t, err)
	require.EqualValues(t, 20, charge.FinalQuota)
	consumption.Usage, consumption.Charge = &usage, charge
	task.Status, task.Progress, task.FinishTime = model.TaskStatusSuccess, "100%", 7230
	task.Data = []byte(`{"result":"completed"}`)
	task.PrivateData.PluginState = []byte(`{"cursor":"final"}`)
	task.PrivateData.CompletionUsage = &model.TaskCompletionUsage{Facts: map[string]any{"calls": "1"}}
	task.PrivateData.BillingContext.Usage, task.PrivateData.BillingContext.Charge = &usage, charge
	won, err := model.PersistTaskCompletion(ctx, &task, model.TaskStatusInProgress, charge.FinalQuota, consumption)
	require.NoError(t, err)
	require.True(t, won)
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("plugin_exit_result_marker", func(tx *gorm.DB) {
		if tx.Statement.Table != "task_result_events" {
			return
		}
		var metric model.PerfMetric
		require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).First(&metric).Error)
		require.EqualValues(t, 1, metric.SuccessCount, "exit after the metric write but before its delivery marker")
		os.Exit(74)
	}))
	require.NoError(t, model.DeliverTaskResultMetrics(ctx, task.ID))
	t.Fatal("the requested result-delivery crash was not reached")
}

func assertTaskPluginExitState(t *testing.T, db *gorm.DB, stage string) {
	t.Helper()
	version := taskPluginExitVersion("1.0.0")
	ref := model.TaskPluginRef{Key: version.Key, Version: version.Version, SourceHash: version.SourceHash}
	var execution model.TaskPluginExecution
	require.NoError(t, db.Where("attempt_id = ?", "plugin-attempt").First(&execution).Error)
	assert.Equal(t, pluginExitRequestID, execution.RequestID)
	assert.Equal(t, ref, model.TaskPluginRef{Key: execution.Key, Version: execution.Version, SourceHash: execution.SourceHash})
	loaded, err := model.GetTaskPluginExact(ref)
	require.NoError(t, err)
	assert.Equal(t, version.Source, loaded.Source)
	// A durable task keeps its executable. A settled attempt without a task
	// no longer pins the old version, so it may be deleted.
	executions := int64(1)
	_, err = model.DeleteTaskPluginVersion(ref.Key, ref.Version)
	if stage == "accepted_before_task" {
		require.NoError(t, err)
		executions = 0
	} else {
		assert.ErrorIs(t, err, model.ErrTaskPluginInUse)
	}
	var recovery model.BillingRecovery
	var attempt model.BillingAttempt
	require.NoError(t, db.Where("request_id = ?", pluginExitRequestID).First(&recovery).Error)
	require.NoError(t, db.Where("id = ?", "plugin-attempt").First(&attempt).Error)
	var quote billing.Quote
	require.NoError(t, common.UnmarshalJsonStr(attempt.Quote, &quote))
	require.NoError(t, model.ValidatePublishedBillingQuote(context.Background(), &quote))
	assert.Equal(t, quote.Hash, attempt.QuoteHash)
	assert.Equal(t, "0.00004", quote.PriceVersion.Pricing["ModelPrice"])
	assert.Equal(t, attempt.Quote, recovery.QuoteSnapshot)
	var attempts, pins int64
	require.NoError(t, db.Model(&model.BillingAttempt{}).Count(&attempts).Error)
	require.NoError(t, db.Model(&model.TaskPluginExecution{}).Count(&pins).Error)
	assert.EqualValues(t, 1, attempts, "recovery retains the original attempt")
	assert.Equal(t, executions, pins)
	var user model.User
	var token model.Token
	require.NoError(t, db.First(&user, 1).Error)
	require.NoError(t, db.First(&token, 1).Error)
	var events []model.BillingEvent
	var logs []model.Log
	var results []model.TaskResultEvent
	var metrics []model.PerfMetric
	require.NoError(t, db.Where("request_id = ?", pluginExitRequestID).Order("id").Find(&events).Error)
	require.NoError(t, db.Where("request_id = ?", pluginExitRequestID).Find(&logs).Error)
	require.NoError(t, db.Find(&results).Error)
	require.NoError(t, db.Find(&metrics).Error)
	// The provider accepted the job in both stages, so recovery charges it once:
	// the frozen per-call estimate, or the persisted completion charge.
	expected := []common.Quota{80, 0, 20}
	assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
	assert.Equal(t, model.BillingRecoveryStateCompleted, recovery.ConsumptionState)
	require.Len(t, events, 2)
	assert.Equal(t, model.BillingRecoveryOperationReserve, events[0].Operation)
	assert.Equal(t, model.BillingRecoveryOperationSettle, events[1].Operation)
	require.Len(t, logs, 1)
	assert.EqualValues(t, 20, logs[0].Quota)
	assert.EqualValues(t, 1, user.RequestCount)
	if stage == "accepted_before_task" {
		var taskCount int64
		require.NoError(t, db.Model(&model.Task{}).Count(&taskCount).Error)
		assert.Zero(t, taskCount)
		assert.Empty(t, execution.TaskID)
		assert.Empty(t, results)
		assert.Empty(t, metrics)
	} else {
		assert.Equal(t, "task-plugin-exit", execution.TaskID)
		require.Len(t, results, 1)
		assert.Equal(t, model.BillingRecoveryStateCompleted, results[0].MetricsState)
		assert.Equal(t, ref.SourceHash, results[0].PluginHash)
		assert.Equal(t, ref.Version, results[0].PluginVersion)
		assert.EqualValues(t, 7230, results[0].FinishTime)
		require.Len(t, metrics, 1)
		assert.EqualValues(t, 1, metrics[0].RequestCount)
		assert.EqualValues(t, 1, metrics[0].SuccessCount)
		assert.EqualValues(t, 30000, metrics[0].TotalLatencyMs)
		assert.EqualValues(t, 20000, metrics[0].GenerationMs)
		var task model.Task
		require.NoError(t, db.First(&task, results[0].TaskID).Error)
		assert.EqualValues(t, model.TaskStatusSuccess, task.Status)
		assert.EqualValues(t, 20, task.Quota)
		assert.Equal(t, &ref, task.PrivateData.PluginRef)
		require.NotNil(t, task.PrivateData.BillingContext)
		require.NotNil(t, task.PrivateData.BillingContext.Quote)
		assert.Equal(t, attempt.QuoteHash, task.PrivateData.BillingContext.Quote.Hash)
		assert.JSONEq(t, `{"cursor":"final"}`, string(task.PrivateData.PluginState))
	}
	assert.Equal(t, expected, []common.Quota{user.Quota, user.ReservedQuota, user.UsedQuota})
	assert.Equal(t, expected, []common.Quota{token.RemainQuota, token.ReservedQuota, token.UsedQuota})
	projection, tokenProjection := []common.Quota{100, 0, 0}, []common.Quota{100, 0, 0}
	for _, event := range events {
		projection[0] += event.AvailableDelta
		projection[1] += event.ReservedDelta
		projection[2] += event.UsedDelta
		tokenProjection[0] += event.TokenAvailableDelta
		tokenProjection[1] += event.TokenReservedDelta
		tokenProjection[2] += event.TokenUsedDelta
	}
	assert.Equal(t, expected, projection)
	assert.Equal(t, expected, tokenProjection)
}
