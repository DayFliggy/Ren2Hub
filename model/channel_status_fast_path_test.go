package model

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestChannelStatusCacheFastPathDatabaseMatrix(t *testing.T) {
	useScheduledChannelDatabase(t)
	old := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = old })
	channel := Channel{Type: 1, Name: "fast-status", Key: "key", Models: "m", Status: common.ChannelStatusAutoDisabled}
	require.NoError(t, channel.Insert())
	require.NoError(t, ReloadChannelCache())
	for _, tc := range []struct {
		name           string
		cached, target int
	}{
		{"already_disabled", common.ChannelStatusAutoDisabled, common.ChannelStatusAutoDisabled},
		{"already_enabled", common.ChannelStatusEnabled, common.ChannelStatusEnabled},
		{"manual_blocks_auto", common.ChannelStatusManuallyDisabled, common.ChannelStatusAutoDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			CacheUpdateChannelStatus(channel.Id, tc.cached)
			var queries atomic.Int32
			require.NoError(t, DB.Callback().Query().Before("gorm:query").Register("fast:no-query", func(tx *gorm.DB) { queries.Add(1); tx.AddError(errors.New("unexpected status query")) }))
			channelStatusLock.Lock()
			done := make(chan ChannelStatusUpdateResult, 1)
			go func() { done <- UpdateChannelStatusAutomatic(channel.Id, tc.target, "already satisfied") }()
			var result ChannelStatusUpdateResult
			fast := false
			select {
			case result = <-done:
				fast = true
			case <-time.After(100 * time.Millisecond):
			}
			channelStatusLock.Unlock()
			if !fast {
				result = <-done
			}
			require.NoError(t, DB.Callback().Query().Remove("fast:no-query"))
			require.True(t, fast, "a cached no-op must not wait behind unrelated status writes")
			require.NoError(t, result.Err)
			require.False(t, result.Changed)
			require.Zero(t, queries.Load(), "a cached no-op must not query storage")
		})
	}
	t.Run("cache_miss_uses_transaction", func(t *testing.T) {
		channelSyncLock.Lock()
		delete(channelsIDM, channel.Id)
		channelSyncLock.Unlock()
		result := UpdateChannelStatusAutomatic(channel.Id, common.ChannelStatusEnabled, "recover")
		require.NoError(t, result.Err)
		require.True(t, result.Changed)
		requireChannelAndAbilities(t, channel.Id, common.ChannelStatusEnabled)
	})
	t.Run("database_remains_write_authority", func(t *testing.T) {
		require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("status", common.ChannelStatusManuallyDisabled).Error)
		CacheUpdateChannelStatus(channel.Id, common.ChannelStatusEnabled)
		result := UpdateChannelStatusAutomatic(channel.Id, common.ChannelStatusAutoDisabled, "stale enabled")
		require.NoError(t, result.Err)
		require.False(t, result.Changed)
		var stored Channel
		require.NoError(t, DB.First(&stored, channel.Id).Error)
		require.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	})
}
