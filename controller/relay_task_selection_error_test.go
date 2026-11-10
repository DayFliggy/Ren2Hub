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

func TestTaskRetrySelectionFailurePreservesVendorError(t *testing.T) {
	for _, failure := range []string{"capacity", "capacity_available", "storage", "cancel_during_selection"} {
		t.Run(failure, func(t *testing.T) {
			withTaskRetries(t, 1)
			events := []string{}
			db := setupTaskSubmissionDatabase(t, true, &events)
			billing := &taskSubmissionTestBilling{events: &events}
			info := taskSubmissionRelayInfo(billing)
			info.LockedChannel = nil
			c := taskSubmissionTestContext()
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			first, err := model.GetChannelById(1, true)
			require.NoError(t, err)
			c.Set("route_selected_channel", first)
			other := &model.Channel{Id: 2201, Type: constant.ChannelTypeTaskPlugin, Name: "retry", Status: common.ChannelStatusEnabled, Models: info.OriginModelName, ChannelRatio: "1", MaxConcurrency: 1}
			require.NoError(t, db.Create(other).Error)
			require.NoError(t, other.AddAbilities(db))
			require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), other.Id))
			var permit *service.ConcurrencyPermit
			if failure == "capacity" {
				permit, err = service.AcquireChannelConcurrency(context.Background(), other)
				require.NoError(t, err)
				defer permit.Release()
			}
			vendor := &dto.TaskError{Code: "vendor_502", Message: "vendor unavailable", StatusCode: http.StatusBadGateway, Data: map[string]any{"trace": "safe-test-trace"}}
			attempts, queryFailed := 0, false
			_, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				service.MarkRouteAttemptSent(c, info)
				if attempts == 2 {
					require.Equal(t, other.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
				}
				if failure == "storage" || failure == "cancel_during_selection" {
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:a4-read", func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
							queryFailed = true
							if failure == "cancel_during_selection" {
								cancel()
							}
							tx.AddError(errors.New("private routing storage failure"))
						}
					}))
				}
				return nil, vendor
			})
			if failure == "capacity_available" {
				require.Equal(t, 2, attempts, "the second channel must be reachable without a competing permit")
			} else {
				require.Equal(t, 1, attempts)
			}
			require.Equal(t, 1, billing.refunds)
			if failure == "cancel_during_selection" {
				require.Equal(t, "request_cancelled", taskErr.Code)
				require.Equal(t, http.StatusRequestTimeout, taskErr.StatusCode)
			} else {
				require.Same(t, vendor, taskErr)
			}
			if failure == "storage" || failure == "cancel_during_selection" {
				require.True(t, queryFailed, "the retry must reach the failing database query")
			}
			require.Zero(t, service.ChannelInFlight(1))
		})
	}
}

func TestTaskLockedRetryKeepsCurrentAdmissionFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{
		{"capacity", http.StatusTooManyRequests, "channel_concurrency_exceeded"},
		{"ratio", http.StatusBadRequest, "channel_ratio_exceeded"},
		{"disabled", http.StatusBadRequest, "origin_task_channel_disabled"},
		{"storage", http.StatusInternalServerError, "get_channel_failed"},
		{"cancel", http.StatusRequestTimeout, "request_cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withTaskRetries(t, 1)
			events := []string{}
			db := setupTaskSubmissionDatabase(t, true, &events)
			billing := &taskSubmissionTestBilling{events: &events}
			info := taskSubmissionRelayInfo(billing)
			c := taskSubmissionTestContext()
			ctx, cancel := context.WithCancel(c.Request.Context())
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
			common.SetContextKey(c, constant.ContextKeyChannelId, 1)
			common.SetContextKey(c, constant.ContextKeyTokenId, 81)
			ceiling := "1"
			require.NoError(t, db.Create(&model.TokenRouteProfile{TokenID: 81, Mode: model.TokenRouteAuto, MaxChannelRatio: &ceiling}).Error)
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("max_concurrency", 1).Error)
			require.NoError(t, service.RefreshChannelConcurrencyLimit(context.Background(), 1))
			var competing *service.ConcurrencyPermit
			defer func() {
				if competing != nil {
					competing.Release()
				}
			}()
			attempts := 0
			_, taskErr := executeTaskSubmissionWith(c, info, func(*gin.Context, *relaycommon.RelayInfo) (*relay.TaskSubmitResult, *dto.TaskError) {
				attempts++
				service.ReleaseRouteChannel(c)
				switch tc.name {
				case "capacity":
					var err error
					competing, err = service.AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 1, MaxConcurrency: 1})
					require.NoError(t, err)
				case "ratio":
					require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("channel_ratio", "2").Error)
				case "disabled":
					require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 1).Update("status", common.ChannelStatusManuallyDisabled).Error)
				case "storage", "cancel":
					require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:locked-retry-read", func(tx *gorm.DB) {
						if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
							if tc.name == "cancel" {
								cancel()
							}
							tx.AddError(errors.New("private origin channel storage failure"))
						}
					}))
				}
				return nil, &dto.TaskError{Code: "vendor_502", Message: "vendor unavailable", StatusCode: http.StatusBadGateway}
			})
			require.Equal(t, 1, attempts)
			require.NotNil(t, taskErr)
			require.Equal(t, tc.status, taskErr.StatusCode)
			require.Equal(t, tc.code, taskErr.Code)
			if tc.name != "cancel" {
				require.True(t, taskErr.NoRetry)
			}
			require.Equal(t, 1, billing.refunds)
			if competing != nil {
				competing.Release()
			}
			require.Zero(t, service.ChannelInFlight(1))
		})
	}
}
