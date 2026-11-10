package controller

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
