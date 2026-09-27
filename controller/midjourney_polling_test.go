package controller

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMidjourneyPoll(t *testing.T) *gorm.DB {
	t.Helper()
	db, _ := setupGatewayAPI(t)
	previousBatch, previousLogging := common.BatchUpdateEnabled, common.LogConsumeEnabled
	common.BatchUpdateEnabled, common.LogConsumeEnabled = false, true
	t.Cleanup(func() { common.BatchUpdateEnabled, common.LogConsumeEnabled = previousBatch, previousLogging })
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Midjourney{}, &model.Token{}, &model.BillingRecovery{}, &model.BillingAttempt{}, &model.BillingEvent{}, &model.QuotaData{}, &model.BillingPriceVersion{}, &model.BillingActivePrice{}))
	require.NoError(t, db.Model(&model.User{}).Where("id = ?", 1).Updates(map[string]any{"available_quota": 100, "used_quota": 0, "request_count": 0}).Error)
	service.InitHttpClient()
	return db
}

func seedMidjourneyPollTask(t *testing.T, db *gorm.DB, endpoint, key, id string) model.Midjourney {
	t.Helper()
	channel := model.Channel{Type: constant.ChannelTypeMidjourneyPlus, Name: key, Key: key, BaseURL: &endpoint, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	task := model.Midjourney{UserId: 1, ChannelId: channel.Id, MjId: id, Status: "SUBMITTED", Progress: "0%", Quota: 20, Action: "IMAGINE", SubmitTime: time.Now().Add(-2 * time.Hour).UnixMilli()}
	require.NoError(t, task.PrivateData.BindCredential(context.Background(), &channel, key))
	task.TokenId = channel.Id
	task.PrivateData.TokenId = channel.Id
	task.PrivateData.BillingSource = "wallet"
	task.PrivateData.SubmissionRequestID = fmt.Sprintf("mj-submit-%d", channel.Id)
	require.NoError(t, db.Create(&model.Token{Id: channel.Id, UserId: 1, Key: fmt.Sprintf("mj-token-%d", channel.Id), RemainQuota: 100}).Error)
	price, err := model.PublishBillingPrice(context.Background(), billing.PriceVersion{
		Model: "mj_imagine", ScopeKind: billing.PriceScopeChannel, ChannelID: channel.Id,
		Mode:       billing.ModeUnitPrice,
		Prices:     []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0.00004"}},
		Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "Reviewed Midjourney polling fixture"},
	}, 0, 1)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*price, "1", nil, time.Now())
	require.NoError(t, err)
	task.PrivateData.BillingContext = &model.TaskBillingContext{Quote: quote, Usage: &billing.Usage{Quantities: map[string]string{"calls": "1"}, Source: "request_quantity", Complete: true}}
	_, err = model.EnsureBillingRecovery(model.BillingRecoveryInput{RequestID: task.PrivateData.SubmissionRequestID, UserID: 1, TokenID: task.TokenId, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, model.Reserve(task.PrivateData.SubmissionRequestID, 20))
	require.NoError(t, model.PrepareBillingAttempt(context.Background(), task.PrivateData.SubmissionRequestID, task.PrivateData.SubmissionRequestID+":attempt", task.ChannelId, quote))
	require.NoError(t, model.PersistAcceptedMidjourney(&task, task.PrivateData.SubmissionRequestID, model.BillingConsumption{Log: model.Log{UserId: 1, TokenId: task.TokenId, ChannelId: channel.Id, Quota: 20}, CountUsage: true, LogEnabled: true}))
	return task
}

func TestMidjourneyPollScopesProviderIDsAndStableCredentials(t *testing.T) {
	db := setupMidjourneyPoll(t)
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		assert.Equal(t, "/mj/task/list-by-condition", r.URL.Path)
		status := "SUCCESS"
		if r.Header.Get("mj-api-secret") == "original" {
			status = "FAILURE"
		} else {
			assert.Equal(t, "other-account", r.Header.Get("mj-api-secret"))
		}
		_, _ = fmt.Fprintf(w, `[{"id":"same-provider-id","status":%q,"progress":"100%%","imageUrl":"https://fixture.invalid/result"}]`, status)
	}))
	defer upstream.Close()
	first := seedMidjourneyPollTask(t, db, upstream.URL, "original", "same-provider-id")
	second := seedMidjourneyPollTask(t, db, upstream.URL, "other-account", "same-provider-id")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", first.ChannelId).Updates(map[string]any{
		"key": "new-first\noriginal", "channel_info": model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2},
	}).Error)
	summary, err := runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, 2, summary.ChannelsScanned)
	require.NoError(t, db.First(&first, first.Id).Error)
	require.NoError(t, db.First(&second, second.Id).Error)
	assert.Equal(t, "FAILURE", first.Status)
	assert.Zero(t, first.Quota)
	assert.Equal(t, "SUCCESS", second.Status)
	assert.EqualValues(t, 20, second.Quota)
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 80, user.Quota)
	assert.EqualValues(t, 20, user.UsedQuota)
	assert.Equal(t, 1, user.RequestCount)
	_, err = runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.EqualValues(t, 2, sends.Load(), "terminal jobs must not be polled or refunded again")
}

func TestMidjourneyPollAmbiguityAndUnavailableOwnerDoNotFailJobs(t *testing.T) {
	for _, scenario := range []string{"missing-status", "duplicate-result", "foreign-id", "http-error", "credential-replaced", "legacy-owner"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupMidjourneyPoll(t)
			var sends atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				switch scenario {
				case "missing-status":
					_, _ = io.WriteString(w, `[{"id":"provider-job","failReason":"polling failed"}]`)
				case "duplicate-result":
					_, _ = io.WriteString(w, `[{"id":"provider-job","status":"FAILURE"},{"id":"provider-job","status":"SUCCESS"}]`)
				case "foreign-id":
					_, _ = io.WriteString(w, `[{"id":"different-job","status":"FAILURE"}]`)
				case "http-error":
					w.WriteHeader(500)
				default:
					t.Error("unavailable owner was sent to provider")
				}
			}))
			defer upstream.Close()
			task := seedMidjourneyPollTask(t, db, upstream.URL, "original", "provider-job")
			if scenario == "credential-replaced" {
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", task.ChannelId).Update("key", "replacement").Error)
			}
			if scenario == "legacy-owner" {
				require.NoError(t, db.Model(&task).Update("private_data", model.TaskPrivateData{}).Error)
			}
			_, err := runMidjourneyTaskUpdateOnce(context.Background(), nil)
			if scenario != "foreign-id" {
				require.Error(t, err, "poll failures must remain visible to the system task")
			}
			require.NoError(t, db.First(&task, task.Id).Error)
			assert.Equal(t, "SUBMITTED", task.Status)
			assert.EqualValues(t, 20, task.Quota)
			var recoveryCount int64
			require.NoError(t, db.Model(&model.BillingRecovery{}).Count(&recoveryCount).Error)
			assert.EqualValues(t, 1, recoveryCount)
			if scenario == "credential-replaced" || scenario == "legacy-owner" {
				assert.Zero(t, sends.Load())
			} else {
				assert.EqualValues(t, 1, sends.Load())
			}
		})
	}
}

func TestMidjourneyPollTerminalLogFailureRecoversWithoutRepoll(t *testing.T) {
	db := setupMidjourneyPoll(t)
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.WriteString(w, `[{"id":"provider-job","status":"FAILURE","failReason":"generation failed"}]`)
	}))
	defer upstream.Close()
	task := seedMidjourneyPollTask(t, db, upstream.URL, "original", "provider-job")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("mj_terminal_log_fault", func(tx *gorm.DB) {
		if tx.Statement.Table == "logs" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("mj_terminal_log_fault") })
	_, err := runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.ErrorIs(t, err, assert.AnError)
	require.NoError(t, db.First(&task, task.Id).Error)
	assert.Equal(t, "FAILURE", task.Status)
	assert.Zero(t, task.Quota)
	_, err = runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.NoError(t, err)
	assert.EqualValues(t, 1, sends.Load())
	require.NoError(t, db.Callback().Create().Remove("mj_terminal_log_fault"))
	for range 2 {
		_, err := service.RecoverStaleBillingRecoveries(context.Background(), time.Now().Unix()+3600, 10)
		require.NoError(t, err)
	}
	var user model.User
	require.NoError(t, db.First(&user, 1).Error)
	assert.EqualValues(t, 100, user.Quota)
	var logs int64
	require.NoError(t, db.Model(&model.Log{}).Where("request_id = ?", task.PrivateData.SubmissionRequestID).Count(&logs).Error)
	assert.EqualValues(t, 1, logs)
}

func TestMidjourneyPollDatabaseFailureIsReported(t *testing.T) {
	db := setupMidjourneyPoll(t)
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("mj_query_fault", func(tx *gorm.DB) {
		if tx.Statement.Table == "midjourneys" {
			tx.AddError(assert.AnError)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Query().Remove("mj_query_fault") })
	_, err := runMidjourneyTaskUpdateOnce(context.Background(), nil)
	require.ErrorIs(t, err, assert.AnError)
}
