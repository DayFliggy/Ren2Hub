package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestManualChannelStatusOverridesStaleCacheDatabaseMatrix(t *testing.T) {
	useScheduledChannelDatabase(t)
	old := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() { common.MemoryCacheEnabled = old })
	for _, target := range []int{common.ChannelStatusEnabled, common.ChannelStatusManuallyDisabled} {
		t.Run(fmt.Sprint(target), func(t *testing.T) {
			stored := common.ChannelStatusEnabled
			if target == stored {
				stored = common.ChannelStatusManuallyDisabled
			}
			channel := Channel{Type: 1, Name: t.Name(), Key: "key", Models: "m", Status: stored}
			require.NoError(t, channel.Insert())
			require.NoError(t, ReloadChannelCache())
			CacheUpdateChannelStatus(channel.Id, target)
			result := UpdateChannelStatusDetailed(channel.Id, "", target, "manual recovery")
			require.NoError(t, result.Err)
			require.True(t, result.Changed, "manual changes must use the durable state even when the cached target matches")
			requireChannelAndAbilities(t, channel.Id, target)
			result = UpdateChannelStatusDetailed(channel.Id, "", target, "repeat")
			require.NoError(t, result.Err)
			require.False(t, result.Changed)
		})
	}
}
