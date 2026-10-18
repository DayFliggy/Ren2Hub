package relay

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestOriginTaskContinuationStillRequiresOriginalOwner(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}))
	require.NoError(t, db.Create(&model.Channel{Id: 11, Key: "origin-test", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}).Error)
	require.NoError(t, db.Create(&model.Task{TaskID: "owned-task", UserId: 17, ChannelId: 11}).Error)
	for _, userID := range []int{17, 18} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/tasks/continue", nil)
		info := &relaycommon.RelayInfo{UserId: userID, OriginModelName: "route-model", TaskRelayInfo: &relaycommon.TaskRelayInfo{OriginTaskID: "owned-task"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 11}}
		before := service.ChannelInFlight(11)
		err := ResolveOriginTask(c, info)
		if userID == 17 {
			require.Nil(t, err)
			locked, ok := info.LockedChannel.(*model.Channel)
			require.True(t, ok)
			require.Equal(t, 11, locked.Id)
		} else {
			require.NotNil(t, err)
			require.Equal(t, http.StatusBadRequest, err.StatusCode)
			require.Equal(t, "task_not_exist", err.Code)
			require.Nil(t, info.LockedChannel)
		}
		require.Nil(t, info.Billing)
		require.Equal(t, before, service.ChannelInFlight(11), "ownership resolution must not acquire capacity or reserve funds")
		require.False(t, c.Writer.Written())
	}
}

func TestOriginTaskStorageFailuresStayPrivateAndDoNotAcquire(t *testing.T) {
	for _, scenario := range []struct {
		name, schema string
		pluginPin    bool
	}{
		{"origin task lookup", "Task", false},
		{"origin channel lookup", "Channel", false},
		{"plugin origin channel lookup", "Channel", true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			db := setupRelayChannelDB(t)
			require.NoError(t, db.AutoMigrate(&model.Task{}))
			require.NoError(t, db.Create(&model.Channel{Id: 11, Key: "origin-test", Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}).Error)
			require.NoError(t, db.Create(&model.Task{TaskID: "owned-task", UserId: 17, ChannelId: 11}).Error)
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:origin-route-storage-failure", func(tx *gorm.DB) {
				if tx.Statement.Schema != nil && tx.Statement.Schema.Name == scenario.schema {
					tx.AddError(errors.New("private storage error fixture"))
				}
			}))
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/tasks/continue", nil)
			info := &relaycommon.RelayInfo{UserId: 17, OriginModelName: "route-model", TaskRelayInfo: &relaycommon.TaskRelayInfo{OriginTaskID: "owned-task"}, ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 11}}
			before := service.ChannelInFlight(11)
			var err *dto.TaskError
			if scenario.pluginPin {
				service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: 11, Source: dto.PinSourceOriginTask, RetryMode: dto.PinRetrySameChannel})
				err = ApplyChannelPin(c, info)
			} else {
				err = ResolveOriginTask(c, info)
			}
			require.NotNil(t, err)
			require.Equal(t, http.StatusInternalServerError, err.StatusCode)
			require.NotContains(t, err.Message, "private storage")
			require.True(t, err.LocalError)
			require.True(t, err.NoRetry)
			require.Nil(t, info.LockedChannel)
			require.Nil(t, info.Billing)
			require.Equal(t, before, service.ChannelInFlight(11))
			require.False(t, c.Writer.Written())
		})
	}
}
