package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTaskPinnedRouteFailureStopsBeforeReservationAndSend(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		status int
		code   string
	}{
		{"storage error", http.StatusInternalServerError, "get_channel_failed"},
		{"disabled original channel", http.StatusBadRequest, "origin_task_channel_disabled"},
		{"original channel full", http.StatusTooManyRequests, "channel_concurrency_exceeded"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			events := []string{}
			db := setupTaskSubmissionDatabase(t, true, &events)
			require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), 1))
			switch scenario.status {
			case http.StatusInternalServerError:
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:pinned-channel-storage-failure", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
						tx.AddError(errors.New("private database failure fixture"))
					}
				}))
			case http.StatusBadRequest:
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
			case http.StatusTooManyRequests:
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("max_concurrency", 1).Error)
				require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), 1))
				permit, err := service.AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 1, MaxConcurrency: 1})
				require.NoError(t, err)
				defer permit.Release()
			}
			before := service.ChannelInFlight(1)
			billing := &taskSubmissionTestBilling{events: &events}
			called := false
			outcome, taskErr := executeTaskSubmissionWith(taskSubmissionTestContext(), taskSubmissionRelayInfo(billing), func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				called = true
				return nil, nil
			})
			require.Nil(t, outcome)
			require.NotNil(t, taskErr)
			require.Equal(t, scenario.status, taskErr.StatusCode)
			require.Equal(t, scenario.code, taskErr.Code)
			require.True(t, taskErr.LocalError)
			require.True(t, taskErr.NoRetry)
			require.NotContains(t, taskErr.Message, "private database")
			require.False(t, called, "failed acquisition must not enter the submission pipeline")
			require.Equal(t, []string{"refund"}, events, "there must be no additional reservation or persisted task")
			require.Equal(t, before, service.ChannelInFlight(1))
		})
	}
}

func TestTaskSubmissionLogsAutomaticScanForAdmins(t *testing.T) {
	events := []string{}
	db := setupTaskSubmissionDatabase(t, true, &events)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	oldLogDB, oldConsume, oldCache, oldRedis := model.LOG_DB, common.LogConsumeEnabled, common.MemoryCacheEnabled, common.RedisEnabled
	model.LOG_DB, common.LogConsumeEnabled, common.MemoryCacheEnabled, common.RedisEnabled = db, true, false, false
	t.Cleanup(func() {
		model.LOG_DB, common.LogConsumeEnabled, common.MemoryCacheEnabled, common.RedisEnabled = oldLogDB, oldConsume, oldCache, oldRedis
	})

	const modelName = "plugin-model"
	primary := model.Channel{Id: 101, Type: constant.ChannelTypeTaskPlugin, Name: "busy", Key: "fixture-busy", Status: common.ChannelStatusEnabled, Models: modelName, ChannelRatio: "1", MaxConcurrency: 1, Priority: common.GetPointer(int64(10))}
	require.NoError(t, db.Create(&primary).Error)
	require.NoError(t, primary.AddAbilities(nil))
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("key", "fixture-backup").Error)
	require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), primary.Id))
	permit, err := service.AcquireChannelConcurrency(context.Background(), &primary)
	require.NoError(t, err)
	defer permit.Release()

	c := taskSubmissionTestContext()
	c.Set(common.RequestIdKey, "task-route-log")
	info := taskSubmissionRelayInfo(&taskSubmissionTestBilling{events: &events})
	info.LockedChannel = nil
	info.TokenId, info.UserId = 81, 71
	outcome, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
		return &relay.TaskSubmitResult{UpstreamTaskID: "fixture-upstream-id", Platform: "plugin"}, nil
	})
	require.Nil(t, taskErr)
	require.NotNil(t, outcome)
	var admin []*model.Log
	require.NoError(t, db.Where("type = ? AND request_id = ?", model.LogTypeConsume, "task-route-log").Find(&admin).Error)
	require.Len(t, admin, 1)
	model.FormatAdminLogs(admin)
	var payload struct {
		Admin struct {
			Policy []service.PolicyEvent `json:"request_policy"`
		} `json:"admin_info"`
	}
	require.NoError(t, common.UnmarshalJsonStr(admin[0].Other, &payload))
	var scans []service.PolicyEvent
	for _, event := range payload.Admin.Policy {
		if event.Decision.Action == "scan" {
			scans = append(scans, event)
		}
	}
	require.Len(t, scans, 1)
	require.Equal(t, "selected", scans[0].Decision.Reason)
	require.Equal(t, 1, scans[0].ChannelID)
	require.NotNil(t, scans[0].Scan)
	require.Equal(t, 2, scans[0].Scan.Candidates)
	require.Equal(t, 1, scans[0].Scan.FullSkips)

	user, err := model.GetLogByTokenId(info.TokenId)
	require.NoError(t, err)
	require.Len(t, user, 1)
	require.NotContains(t, user[0].Other, "request_policy")
}
