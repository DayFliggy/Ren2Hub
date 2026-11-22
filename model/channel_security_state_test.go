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

// Uses the task database harness so the same transaction contract runs on all three databases.
func TestChannelStatusFailureDatabaseMatrix(t *testing.T) {
	useScheduledChannelDatabase(t)
	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })
	channel := Channel{Type: 1, Name: "status-failure", Key: "key", Models: "test-model", Status: common.ChannelStatusEnabled}
	require.NoError(t, channel.Insert())
	InitChannelCache()
	t.Run("read failure retains cache and recovery publishes", func(t *testing.T) {
		require.NoError(t, DB.Callback().Query().Before("gorm:query").Register("test:channel_read_failure", func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
				tx.AddError(errors.New("channel read failed"))
			}
		}))
		require.Error(t, ReloadChannelCache())
		cached, err := CacheGetChannel(channel.Id)
		require.NoError(t, DB.Callback().Query().Remove("test:channel_read_failure"))
		require.NoError(t, err)
		require.Equal(t, common.ChannelStatusEnabled, cached.Status)
		require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("name", "recovered").Error)
		InitChannelCache()
		cached, err = CacheGetChannel(channel.Id)
		require.NoError(t, err)
		require.Equal(t, "recovered", cached.Name)
	})
	t.Run("rollback leaves cache and abilities unchanged", func(t *testing.T) {
		require.NoError(t, ReloadChannelCache())
		failAbilityWrites(t)
		result := UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusManuallyDisabled, "test")
		require.False(t, result.Changed)
		require.Error(t, result.Err)
		cached, err := CacheGetChannel(channel.Id)
		require.NoError(t, err)
		require.Equal(t, common.ChannelStatusEnabled, cached.Status)
		requireChannelAndAbilities(t, channel.Id, common.ChannelStatusEnabled)
	})
	t.Run("commit publishes after transaction and reload repairs cached no-op", func(t *testing.T) {
		require.NoError(t, DB.Callback().Update().After("gorm:update").Register("test:check_uncommitted_cache", func(tx *gorm.DB) {
			if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
				cached, err := CacheGetChannel(channel.Id)
				require.NoError(t, err)
				require.Equal(t, common.ChannelStatusEnabled, cached.Status, "uncommitted disable must not publish")
			}
		}))
		result := UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusManuallyDisabled, "commit")
		require.NoError(t, DB.Callback().Update().Remove("test:check_uncommitted_cache"))
		require.NoError(t, result.Err)
		require.True(t, result.Changed)
		requireChannelAndAbilities(t, channel.Id, common.ChannelStatusManuallyDisabled)
		cached, err := CacheGetChannel(channel.Id)
		require.NoError(t, err)
		require.Equal(t, common.ChannelStatusManuallyDisabled, cached.Status)
		// Simulate a stale snapshot claiming enabled while durable state is disabled.
		CacheUpdateChannelStatus(channel.Id, common.ChannelStatusEnabled)
		result = UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusEnabled, "recover")
		require.NoError(t, result.Err)
		require.False(t, result.Changed, "a cached no-op avoids querying the durable row")
		requireChannelAndAbilities(t, channel.Id, common.ChannelStatusManuallyDisabled)
		require.NoError(t, ReloadChannelCache())
		result = UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusEnabled, "recover after reload")
		require.NoError(t, result.Err)
		require.True(t, result.Changed)
		requireChannelAndAbilities(t, channel.Id, common.ChannelStatusEnabled)
		result = UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusEnabled, "unchanged")
		require.NoError(t, result.Err)
		require.False(t, result.Changed)
		result = UpdateChannelStatusDetailed(channel.Id+100000, "", common.ChannelStatusEnabled, "missing")
		require.ErrorIs(t, result.Err, gorm.ErrRecordNotFound)
	})

}

func TestChannelStatusCacheSnapshotsAreImmutable(t *testing.T) {
	useScheduledChannelDatabase(t)
	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })
	channel := Channel{Type: 1, Name: "immutable", Key: "key", Models: "test-model", Status: common.ChannelStatusEnabled}
	require.NoError(t, channel.Insert())
	require.NoError(t, ReloadChannelCache())
	old, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	started, done := make(chan struct{}), make(chan struct{})
	stop := make(chan struct{})
	var observed atomic.Int64
	go func() {
		close(started)
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				observed.Store(int64(old.Status))
			}
		}
	}()
	<-started
	for i := 0; i < 1000; i++ {
		CacheUpdateChannelStatus(channel.Id, common.ChannelStatusManuallyDisabled)
		CacheUpdateChannelStatus(channel.Id, common.ChannelStatusEnabled)
	}
	CacheUpdateChannelStatus(channel.Id, common.ChannelStatusManuallyDisabled)
	close(stop)
	<-done
	require.Equal(t, common.ChannelStatusEnabled, old.Status, "a reader's snapshot must not mutate")
	current, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusManuallyDisabled, current.Status)
}

func TestChannelCacheReloadSerializesStatusPublication(t *testing.T) {
	useScheduledChannelDatabase(t)
	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })
	channel := Channel{Type: 1, Name: "ordered-publication", Key: "key", Models: "test-model", Status: common.ChannelStatusEnabled}
	require.NoError(t, channel.Insert())
	require.NoError(t, ReloadChannelCache())
	read, resume := make(chan struct{}), make(chan struct{})
	var paused atomic.Bool
	require.NoError(t, DB.Callback().Query().After("gorm:query").Register("test:pause_cache_snapshot", func(tx *gorm.DB) {
		if _, ok := tx.Statement.Dest.(*[]*Channel); ok && paused.CompareAndSwap(false, true) {
			close(read)
			<-resume
		}
	}))
	t.Cleanup(func() { DB.Callback().Query().Remove("test:pause_cache_snapshot") })
	reloadDone := make(chan error, 1)
	go func() { reloadDone <- ReloadChannelCache() }()
	<-read
	statusDone := make(chan ChannelStatusUpdateResult, 1)
	go func() {
		statusDone <- UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusManuallyDisabled, "interleaved")
	}()
	var result ChannelStatusUpdateResult
	completed := false
	select {
	case result = <-statusDone:
		completed = true
	case <-time.After(100 * time.Millisecond):
	}
	close(resume)
	require.NoError(t, <-reloadDone)
	if !completed {
		result = <-statusDone
	}
	require.NoError(t, result.Err)
	require.True(t, result.Changed)
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusManuallyDisabled, cached.Status, "an earlier reload cannot overwrite a later committed status")
	requireChannelAndAbilities(t, channel.Id, common.ChannelStatusManuallyDisabled)
	// Post-commit hooks may reload without recursively acquiring the status lock.
	recordChannelPublications(t, func(int) error { return ReloadChannelCache() })
	result = UpdateChannelStatusDetailed(channel.Id, "", common.ChannelStatusEnabled, "callback reload")
	require.NoError(t, result.Err)
	require.False(t, result.PublicationPending)
	require.True(t, result.Changed)
}
