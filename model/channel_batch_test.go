package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchInsertChannelsNotifiesCommittedIDs(t *testing.T) {
	db := useOpsTestDB(t, "batch-channels.db")
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	capabilityRefreshHook.RLock()
	previousHook := capabilityRefreshHook.fn
	capabilityRefreshHook.RUnlock()
	t.Cleanup(func() { SetChannelCapabilityRefreshHook(previousHook) })

	var notified []int
	SetChannelCapabilityRefreshHook(func(id int) {
		notified = append(notified, id)
		// The hook observes committed rows through the regular connection.
		var channel Channel
		require.NoError(t, db.First(&channel, id).Error)
		var ability Ability
		require.NoError(t, db.Where("channel_id = ?", id).First(&ability).Error)
		assert.Equal(t, channel.Models, ability.Model)
	})

	channels := make([]Channel, 51) // Cover notification across the batch boundary.
	for index := range channels {
		channels[index] = Channel{
			Type: constant.ChannelTypeOpenAI, Name: fmt.Sprintf("batch-%d", index),
			Key: "local-fixture-key", Models: "gpt-4o-mini", Group: "default", Status: common.ChannelStatusEnabled,
		}
	}
	require.NoError(t, BatchInsertChannels(channels))
	require.Len(t, notified, len(channels))
	for index, channel := range channels {
		assert.Positive(t, channel.Id, "database IDs must return to the caller")
		assert.Equal(t, channel.Id, notified[index])
	}

	notified = nil
	failed := make([]Channel, 51)
	copy(failed, channels)
	for index := range failed {
		failed[index].Id = 0
		failed[index].Name = fmt.Sprintf("rollback-%d", index)
	}
	failed[50].Type = constant.ChannelTypeCodex
	require.ErrorIs(t, BatchInsertChannels(failed), constant.ErrRetiredChannelType)
	assert.Empty(t, notified, "a later batch failure must not publish any changes")
	var count int64
	require.NoError(t, db.Model(&Channel{}).Count(&count).Error)
	assert.EqualValues(t, len(channels), count, "all inserts from the failed transaction roll back")
	require.NoError(t, db.Model(&Ability{}).Count(&count).Error)
	assert.EqualValues(t, len(channels), count)
}
