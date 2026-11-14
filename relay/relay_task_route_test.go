package relay

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
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

// Writing LockedChannel for a plugin origin pin reads the channel the same
// way with the memory cache on or off. Before the fix a channel missing only
// from the cache was a 500 in cache mode.
