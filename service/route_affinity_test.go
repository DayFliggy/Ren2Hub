package service

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// enableRouteAffinity binds the X-Session header of the current test to channelID.
func enableRouteAffinity(t *testing.T, mode string, channelID int) (*gin.Context, string) {
	t.Helper()
	setting := operation_setting.GetChannelAffinitySetting()
	previous := *setting
	t.Cleanup(func() { *setting = previous })
	rule := operation_setting.ChannelAffinityRule{
		Name: "route-affinity", ModelRegex: []string{"^route-model$"}, SessionMode: mode,
		KeySources: []operation_setting.ChannelAffinityKeySource{{Type: "request_header", Key: "X-Session"}},
	}
	encoded, err := common.Marshal([]operation_setting.ChannelAffinityRule{rule})
	require.NoError(t, err)
	snapshot, err := model.BuildRequestPolicy(map[string]string{"channel_affinity_setting.enabled": "true", "channel_affinity_setting.session_mode": mode, "channel_affinity_setting.rules": string(encoded)})
	require.NoError(t, err)
	*setting = snapshot.Affinity
	key := buildChannelAffinityCacheKeySuffix(rule, "route-model", t.Name())
	cache := getChannelAffinityCache()
	require.NoError(t, cache.SetWithTTL(key, channelID, time.Minute))
	t.Cleanup(func() { _, _ = cache.DeleteMany([]string{key}) })
	c := routeTestContext(t)
	c.Request.Header.Set("X-Session", t.Name())
	return c, key
}

func affinityBinding(t *testing.T, key string) (int, bool) {
	t.Helper()
	channelID, found, err := getChannelAffinityCache().Get(key)
	require.NoError(t, err)
	return channelID, found
}

// The bound channel has the lower priority, so only a working affinity selects it.
func setupAffinityChannels(t *testing.T) *gorm.DB {
	t.Helper()
	db := setupRouteTest(t)
	createRouteChannel(t, db, 11, "route-model")
	createRouteChannel(t, db, 22, "route-model")
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 11).Update("priority", 10).Error)
	return db
}

// deleteAfterCandidateRead 在第一次读出候选渠道之后、准入之前删除渠道 id 并完成并发清理，
// 与管理员删除和选路交错的时序相同。返回值报告删除是否已经发生。
func deleteAfterCandidateRead(t *testing.T, db *gorm.DB, id int) *atomic.Bool {
	t.Helper()
	var deleted atomic.Bool
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:delete-after-candidates", func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" && !tx.DryRun && deleted.CompareAndSwap(false, true) {
			require.NoError(t, tx.Session(&gorm.Session{NewDB: true}).Delete(&model.Channel{}, id).Error)
			gatewayConcurrency.forget("channel", []int{id})
		}
	}))
	return &deleted
}

func TestPreferAffinityUsesBoundChannelAndFallsBackWhenUnqualified(t *testing.T) {
	t.Run("bound channel wins over priority", func(t *testing.T) {
		setupAffinityChannels(t)
		c, key := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 22, selected.Id)
		_, logged := c.Get(ginKeyChannelAffinityLogInfo)
		require.True(t, logged, "a used binding is recorded in the consume log")
		bound, found := affinityBinding(t, key)
		require.True(t, found)
		require.Equal(t, 22, bound)
	})
	t.Run("disabled bound channel falls back and clears binding", func(t *testing.T) {
		db := setupAffinityChannels(t)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("status", common.ChannelStatusManuallyDisabled).Error)
		c, key := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 11, selected.Id)
		_, found := affinityBinding(t, key)
		require.False(t, found)
	})
	t.Run("full bound channel falls back and keeps binding", func(t *testing.T) {
		db := setupAffinityChannels(t)
		require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("max_concurrency", 1).Error)
		holder, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 22, MaxConcurrency: 1})
		require.NoError(t, err)
		defer holder.Release()
		c, key := enableRouteAffinity(t, "prefer", 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.Equal(t, 11, selected.Id)
		bound, found := affinityBinding(t, key)
		require.True(t, found, "capacity is transient and must not drop the session binding")
		require.Equal(t, 22, bound)
	})
	t.Run("bound channel deleted after the candidate read falls back", func(t *testing.T) {
		db := setupAffinityChannels(t)
		c, key := enableRouteAffinity(t, "prefer", 22)
		deleted := deleteAfterCandidateRead(t, db, 22)
		selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.Nil(t, selectErr)
		require.True(t, deleted.Load())
		require.Equal(t, 11, selected.Id)
		require.Zero(t, ChannelInFlight(22), "the deleted channel received no permit")
		_, found := affinityBinding(t, key)
		require.False(t, found)
	})
	t.Run("storage failure is reported, not treated as unqualified", func(t *testing.T) {
		db := setupAffinityChannels(t)
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("routing:affinity-error", func(tx *gorm.DB) {
			if tx.Statement.Table == "channels" {
				tx.AddError(errors.New("affinity storage unavailable"))
			}
		}))
		c, key := enableRouteAffinity(t, "prefer", 22)
		_, selectErr := SelectChannelForRequest(c, "route-model", nil)
		require.NotNil(t, selectErr)
		require.Equal(t, http.StatusInternalServerError, selectErr.StatusCode)
		_, found := affinityBinding(t, key)
		require.True(t, found)
	})
}

func TestStrictAffinityStillRejectsUnavailableBoundChannel(t *testing.T) {
	for name, unavailable := range map[string]func(*testing.T, *gorm.DB){
		"disabled": func(t *testing.T, db *gorm.DB) {
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("status", common.ChannelStatusManuallyDisabled).Error)
		},
		"deleted after the candidate read": func(t *testing.T, db *gorm.DB) { deleteAfterCandidateRead(t, db, 22) },
	} {
		t.Run(name, func(t *testing.T) {
			db := setupAffinityChannels(t)
			c, _ := enableRouteAffinity(t, "strict", 22)
			unavailable(t, db)
			_, selectErr := SelectChannelForRequest(c, "route-model", nil)
			require.NotNil(t, selectErr)
			require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
		})
	}
}

// A shared endpoint pre-selects before user admission and reacquires after
// it. When the reacquire loses the bound channel, the affinity use recorded
// by the pre-selection must not survive into admin_info or retry policy.
func TestReacquireSelectedRouteChannelForgetsLostAffinityUse(t *testing.T) {
	db := setupAffinityChannels(t)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("max_concurrency", 1).Error)
	c, _ := enableRouteAffinity(t, "prefer", 22)
	selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 22, selected.Id)
	ReleaseRouteChannel(c)
	holder, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 22, MaxConcurrency: 1})
	require.NoError(t, err)
	defer holder.Release()

	reacquired, selectErr := ReacquireSelectedRouteChannel(c, 22, "route-model")
	require.Nil(t, selectErr)
	require.Nil(t, reacquired)
	info, _ := c.Get(ginKeyChannelAffinityLogInfo)
	require.Nil(t, info)
	selected, selectErr = SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selectErr)
	require.Equal(t, 11, selected.Id)
	info, _ = c.Get(ginKeyChannelAffinityLogInfo)
	require.Nil(t, info, "the fallback channel was not chosen by affinity")
	ReleaseRouteChannel(c)
	require.Equal(t, 1, ChannelInFlight(22))
	require.Zero(t, ChannelInFlight(11))
}

func TestStrictAffinityUnavailableUsesNativeError(t *testing.T) {
	db := setupAffinityChannels(t)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("status", common.ChannelStatusManuallyDisabled).Error)
	c, _ := enableRouteAffinity(t, "strict", 22)
	selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selected)
	require.NotNil(t, selectErr)
	require.Equal(t, http.StatusServiceUnavailable, selectErr.StatusCode)
	require.Empty(t, selectErr.Code)
	require.Equal(t, "strict_session_binding_unavailable", selectErr.Message)
}

func TestStrictAffinityFullChannelKeepsBindingAndReturns429(t *testing.T) {
	db := setupAffinityChannels(t)
	require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 22).Update("max_concurrency", 1).Error)
	holder, err := AcquireChannelConcurrency(context.Background(), &model.Channel{Id: 22, MaxConcurrency: 1})
	require.NoError(t, err)
	defer holder.Release()
	c, key := enableRouteAffinity(t, "strict", 22)
	selected, selectErr := SelectChannelForRequest(c, "route-model", nil)
	require.Nil(t, selected)
	require.NotNil(t, selectErr)
	require.Equal(t, http.StatusTooManyRequests, selectErr.StatusCode)
	bound, found := affinityBinding(t, key)
	require.True(t, found)
	require.Equal(t, 22, bound)
	require.Equal(t, 1, ChannelInFlight(22), "only the pre-existing borrower holds a slot")
	require.Zero(t, ChannelInFlight(11), "strict binding cannot switch to another channel")
}
