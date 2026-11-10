package service

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func originTaskPinContext(t *testing.T, channelID int) *gin.Context {
	t.Helper()
	c := routeTestContext(t)
	GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: channelID, Source: dto.PinSourceOriginTask, Rank: dto.PinRankOriginTask, RetryMode: dto.PinRetrySameChannel})
	return c
}

// An accepted task continues on its original channel after the Key's manual
// list stops naming it. The price limit is tested separately below.
func TestOriginTaskPinKeepsOriginalChannelDespiteKeyRoute(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "video-model")
	createRouteChannel(t, db, 22, "video-model")
	saveRouteTest(t, 22)

	selected, selectErr := SelectChannelForRequest(originTaskPinContext(t, 11), "video-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 11, selected.Id)

	ordinary, selectErr := SelectChannelForRequest(routeTestContext(t), "video-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 22, ordinary.Id, "a request without an origin task still follows the manual list")
}

func TestOriginTaskPinHonorsKeyPriceLimit(t *testing.T) {
	for _, mode := range []string{model.TokenRouteAuto, model.TokenRouteManual} {
		for _, tc := range []struct {
			name    string
			ceiling *string
			denied  bool
		}{
			{name: "no limit"},
			{name: "below limit", ceiling: common.GetPointer("3")},
			{name: "equal to limit", ceiling: common.GetPointer("2")},
			{name: "above limit", ceiling: common.GetPointer("1"), denied: true},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				db := setupRouteTest(t)
				createRouteChannel(t, db, 11, "video-model")
				createRouteChannel(t, db, 22, "video-model")
				saveRouteTest(t, 22)
				require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("channel_ratio", "2").Error)
				require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Updates(map[string]any{
					"mode": mode, "max_channel_ratio": tc.ceiling,
				}).Error)
				c := originTaskPinContext(t, 11)

				selected, selectErr := SelectChannelForRequest(c, "video-model", nil)

				if !tc.denied {
					require.Nil(t, selectErr)
					require.Equal(t, 11, selected.Id, "the manual list does not move a continuation to channel 22")
					ReleaseRouteChannel(c)
				} else {
					require.Nil(t, selected)
					require.NotNil(t, selectErr)
					require.Equal(t, http.StatusBadRequest, selectErr.StatusCode)
					require.EqualValues(t, "channel_ratio_exceeded", selectErr.Code)
					_, err := AcquireOriginTaskRouteChannel(c, 11, "video-model")
					taskErr := PinnedTaskRouteError(err)
					require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
					require.Equal(t, "channel_ratio_exceeded", taskErr.Code)
					require.True(t, taskErr.NoRetry)
				}
				require.Zero(t, ChannelInFlight(11))
				require.Zero(t, ChannelInFlight(22), "a cheaper channel must never replace the origin")
			})
		}
	}
}

func TestOriginTaskPricePolicyFailureIsNotAnExemption(t *testing.T) {
	for _, failure := range []string{"storage failure", "invalid ceiling"} {
		t.Run(failure, func(t *testing.T) {
			db := setupRouteTest(t)
			createRouteChannel(t, db, 11, "video-model")
			saveRouteTest(t, 11)
			if failure == "storage failure" {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:origin-policy-storage", func(tx *gorm.DB) {
					if tx.Statement.Table == "token_route_profiles" {
						tx.AddError(errors.New("private policy storage failure"))
					}
				}))
			} else {
				require.NoError(t, db.Model(&model.TokenRouteProfile{}).Where("token_id = ?", 7).Update("max_channel_ratio", "invalid").Error)
			}

			selected, selectErr := SelectChannelForRequest(originTaskPinContext(t, 11), "video-model", nil)

			require.Nil(t, selected)
			require.NotNil(t, selectErr)
			require.Equal(t, http.StatusInternalServerError, selectErr.StatusCode)
			require.NotContains(t, selectErr.Message, "private")
			require.Zero(t, ChannelInFlight(11))
		})
	}
}

// Explicit request constraints still apply to the origin channel, but the
// ordinary-request default that excludes type-61 channels does not.
func TestOriginTaskPinKeepsExplicitRequestConstraints(t *testing.T) {
	db := setupRouteTest(t)
	setting := `{"task_plugin_key":"sora"}`
	plugin := model.Channel{Id: 31, Type: constant.ChannelTypeTaskPlugin, Key: "k", Name: "sora", Models: "video-model", Status: common.ChannelStatusEnabled, Setting: &setting}
	require.NoError(t, db.Create(&plugin).Error)
	require.NoError(t, plugin.AddAbilities(nil))

	c := originTaskPinContext(t, 31)
	AppendTaskPluginIdentityFilter(c, "")
	selected, selectErr := SelectChannelForRequest(c, "video-model", nil)
	require.Nil(t, selectErr, "the default no-type-61 filter is not a constraint on the task")
	require.Equal(t, 31, selected.Id)
	ReleaseRouteChannel(c)

	for name, filter := range map[string]dto.ChannelFilter{
		"another plugin":   {Kind: dto.FilterTaskPluginIdentity, TaskPluginKey: "kling"},
		"websocket create": {Kind: dto.FilterResponsesWebSocket},
	} {
		t.Run(name, func(t *testing.T) {
			c := originTaskPinContext(t, 31)
			GetChannelConstraints(c).AddFilter(filter)
			selected, selectErr := SelectChannelForRequest(c, "video-model", nil)
			require.Nil(t, selected)
			require.NotNil(t, selectErr)
			require.Equal(t, http.StatusBadRequest, selectErr.StatusCode)
			require.Equal(t, types.ErrorCode(filter.Kind), selectErr.Code)
			require.Zero(t, ChannelInFlight(31))
		})
	}
}

// The origin task's channel is deleted only when the database says so; a
// memory-cache miss is read through, and a storage failure stays a storage
// failure (audit B14 on the origin-task chain).
func TestLoadOriginTaskChannelSeparatesDeletionCacheMissAndStorageFailure(t *testing.T) {
	for _, cache := range []bool{false, true} {
		t.Run(fmt.Sprint("cache=", cache), func(t *testing.T) {
			db := setupRouteTest(t)
			common.MemoryCacheEnabled = cache
			model.InitChannelCache()
			createRouteChannel(t, db, 11, "video-model")

			channel, err := LoadOriginTaskChannel(11)
			require.NoError(t, err, "a channel created after the cache was built is read from the database")
			require.Equal(t, 11, channel.Id)

			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
			model.InitChannelCache()
			_, err = LoadOriginTaskChannel(11)
			require.True(t, errors.Is(err, errOriginTaskChannelUnavailable))
			require.Equal(t, http.StatusBadRequest, PinnedTaskRouteError(err).StatusCode)
			require.Equal(t, "origin_task_channel_disabled", PinnedTaskRouteError(err).Code)

			_, err = LoadOriginTaskChannel(99)
			require.True(t, errors.Is(err, errOriginTaskChannelUnavailable), "the database confirms the channel is gone")

			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:origin-channel-storage", func(tx *gorm.DB) {
				if tx.Statement.Table == "channels" {
					tx.AddError(errors.New("private origin channel storage failure"))
				}
			}))
			_, err = LoadOriginTaskChannel(12)
			require.Error(t, err)
			require.False(t, errors.Is(err, errOriginTaskChannelUnavailable))
			taskErr := PinnedTaskRouteError(err)
			require.Equal(t, http.StatusInternalServerError, taskErr.StatusCode)
			require.NotContains(t, taskErr.Message, "private")
		})
	}
}

// Admission of the origin channel: deleted or disabled is the origin task's
// own 400; capacity and storage keep the shared mapping.
func TestAcquireOriginTaskRouteChannelErrorTable(t *testing.T) {
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "video-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("status", common.ChannelStatusManuallyDisabled).Error)
	_, err := AcquireOriginTaskRouteChannel(routeTestContext(t), 11, "video-model")
	taskErr := PinnedTaskRouteError(err)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Equal(t, "origin_task_channel_disabled", taskErr.Code)
	require.True(t, taskErr.NoRetry)

	_, err = AcquireOriginTaskRouteChannel(routeTestContext(t), 404, "video-model")
	require.Equal(t, "origin_task_channel_disabled", PinnedTaskRouteError(err).Code)

	selected, selectErr := SelectChannelForRequest(originTaskPinContext(t, 11), "video-model", nil)
	require.Nil(t, selected)
	require.Equal(t, http.StatusBadRequest, selectErr.StatusCode)
	require.EqualValues(t, "origin_task_channel_disabled", selectErr.Code)
}
