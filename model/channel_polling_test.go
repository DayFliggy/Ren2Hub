package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pollingTestChannel(t *testing.T, memory bool) *Channel {
	t.Helper()
	setupChannelStatusTest(t)
	channelSyncLock.Lock()
	channelsIDM = nil
	channelSyncLock.Unlock()
	common.MemoryCacheEnabled = memory
	channel := &Channel{Name: "polling", Type: 1, Key: "key-a\nkey-b\nkey-c", Models: "polling-model", Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{IsMultiKey: true, MultiKeySize: 3, MultiKeyMode: constant.MultiKeyModePolling}}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{Model: "polling-model", ChannelId: channel.Id, Enabled: true, Weight: 1}).Error)
	InitChannelCache()
	return channel
}

func nextPollingCandidate(t *testing.T) int {
	t.Helper()
	rows, err := RouteChannelRows(context.Background(), "polling-model", nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	_, index, keyErr := rows[0].GetNextEnabledKey()
	require.Nil(t, keyErr)
	return index
}

func TestPollingFreshRouteCandidatesAndCacheSync(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache_%v", memory), func(t *testing.T) {
			pollingTestChannel(t, memory)
			for i := 0; i < 18; i++ {
				if i == 8 {
					InitChannelCache()
				}
				require.Equal(t, i%3, nextPollingCandidate(t))
			}
		})
	}
}

func TestPollingSkipsDisabledKeysAndHandlesShortenedKeyList(t *testing.T) {
	for _, memory := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache_%v", memory), func(t *testing.T) {
			channel := pollingTestChannel(t, memory)
			require.True(t, UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusManuallyDisabled, "test"))
			for _, expected := range []int{0, 2, 0, 2, 0} {
				require.Equal(t, expected, nextPollingCandidate(t))
			}
			// The current cursor is 1, which is invalid after shrinking to one key.
			require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Update("key", "key-a").Error)
			InitChannelCache()
			require.Equal(t, 0, nextPollingCandidate(t))
		})
	}
}

func TestPollingConcurrentRequestsAndCacheRebuild(t *testing.T) {
	channel := pollingTestChannel(t, true)
	var wg sync.WaitGroup
	results := make(chan int, 90)
	errors := make(chan error, 90)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 12; i++ {
			InitChannelCache()
		}
	}()
	for i := 0; i < 90; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Exercise both cache-owned and fresh DB receivers.
			var candidate *Channel
			var err error
			if i%2 == 0 {
				candidate, err = CacheGetChannel(channel.Id)
			} else {
				var rows []*Channel
				rows, err = RouteChannelRows(context.Background(), "polling-model", nil)
				if err == nil && len(rows) == 1 {
					candidate = rows[0]
				}
			}
			if candidate == nil && err == nil {
				err = fmt.Errorf("missing candidate")
			}
			if err != nil {
				errors <- err
				return
			}
			_, index, keyErr := candidate.GetNextEnabledKey()
			if keyErr != nil {
				errors <- keyErr
				return
			}
			results <- index
		}(i)
	}
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	counts := [3]int{}
	for index := range results {
		counts[index]++
	}
	require.Equal(t, [3]int{30, 30, 30}, counts)
}

func TestPollingDoesNotLoseConcurrentMultiKeyStatus(t *testing.T) {
	channel := pollingTestChannel(t, true)
	var wg sync.WaitGroup
	errors := make(chan error, 101)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			candidate, err := CacheGetChannel(channel.Id)
			if err != nil {
				errors <- err
				return
			}
			_, index, keyErr := candidate.GetNextEnabledKey()
			if keyErr != nil || index < 0 {
				if keyErr != nil {
					errors <- keyErr
				} else {
					errors <- fmt.Errorf("polling returned a negative key index")
				}
				return
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 20 {
			UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusManuallyDisabled, "concurrent test")
			UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusEnabled, "concurrent test")
		}
		UpdateChannelStatus(channel.Id, "key-b", common.ChannelStatusManuallyDisabled, "concurrent test")
	}()
	wg.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	info, err := CacheGetChannelInfo(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, info.MultiKeyStatusList[1])
	beforeCursor := info.MultiKeyPollingIndex
	UpdateChannelStatus(channel.Id, "key-c", common.ChannelStatusManuallyDisabled, "preserve cursor")
	info, err = CacheGetChannelInfo(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, beforeCursor, info.MultiKeyPollingIndex)
	InitChannelCache()
	info, err = CacheGetChannelInfo(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, info.MultiKeyStatusList[1])
	assert.Equal(t, beforeCursor, info.MultiKeyPollingIndex)
}

func TestPollingCursorPersistenceFailureStillSelectsKey(t *testing.T) {
	channel := pollingTestChannel(t, false)
	const callbackName = "test:fail_channel_polling_cursor_persistence"
	require.NoError(t, DB.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table == "channels" {
			tx.AddError(errors.New("injected polling cursor persistence failure"))
		}
	}))
	t.Cleanup(func() {
		if err := DB.Callback().Update().Remove(callbackName); err != nil {
			t.Errorf("failed to remove GORM test callback: %v", err)
		}
	})

	key, index, err := channel.GetNextEnabledKey()
	require.Nil(t, err)
	assert.Equal(t, "key-a", key)
	assert.Equal(t, 0, index)
}

func TestPollingReadFailureSkipsRetry(t *testing.T) {
	for _, failure := range []string{"cache_owner_missing", "database_row_missing", "database_query_failure"} {
		t.Run(failure, func(t *testing.T) {
			channel := pollingTestChannel(t, failure == "cache_owner_missing")
			switch failure {
			case "cache_owner_missing":
				channelSyncLock.Lock()
				delete(channelsIDM, channel.Id)
				channelSyncLock.Unlock()
			case "database_row_missing":
				require.NoError(t, DB.Delete(&Channel{}, channel.Id).Error)
			case "database_query_failure":
				const callback = "test:fail_polling_cursor_read"
				require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
						tx.AddError(errors.New("injected polling cursor read failure"))
					}
				}))
				t.Cleanup(func() { assert.NoError(t, DB.Callback().Query().Remove(callback)) })
			}
			key, index, apiErr := channel.GetNextEnabledKey()
			require.NotNil(t, apiErr)
			require.Empty(t, key)
			require.Zero(t, index)
			require.Equal(t, types.ErrorCodeGetChannelFailed, apiErr.GetErrorCode())
			require.Equal(t, 500, apiErr.StatusCode)
			require.True(t, types.IsSkipRetryError(apiErr), "polling cursor lookup errors retain the native non-retryable flag")
		})
	}
}
