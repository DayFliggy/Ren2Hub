package model

import (
	"errors"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
)

func TestRestoredRound3ChannelStatusFailureRestoresCache(t *testing.T) {
	useScheduledChannelDatabase(t)
	db := DB
	oldMemory := common.MemoryCacheEnabled
	t.Cleanup(func() { common.MemoryCacheEnabled = oldMemory })
	channel := Channel{Id: 713, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "fixture", Key: "fixture", Models: "m"}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(db))
	common.MemoryCacheEnabled = true
	InitChannelCache()
	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, cached.Status)
	fault := errors.New("round3 controlled channel write failure")
	var hits int
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("round3:fail-channel", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
			hits++
			tx.AddError(fault)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove("round3:fail-channel") })
	changed := UpdateChannelStatus(channel.Id, "fixture", common.ChannelStatusAutoDisabled, "controlled failure")
	require.False(t, changed)
	require.Equal(t, 1, hits)
	var stored Channel
	require.NoError(t, db.First(&stored, channel.Id).Error)
	require.Equal(t, common.ChannelStatusEnabled, stored.Status)
	cached, err = CacheGetChannel(channel.Id)
	require.NoError(t, err)
	assert.Equal(t, common.ChannelStatusEnabled, cached.Status, "a rejected database write must not leave a disabled cache row")
}
