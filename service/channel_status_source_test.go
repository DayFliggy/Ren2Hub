package service

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAutomaticChannelStatusSourcesSkipCachedNoOps(t *testing.T) {
	for _, status := range []int{common.ChannelStatusEnabled, common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			db := setupAffinityChannels(t)
			common.MemoryCacheEnabled = true
			require.NoError(t, model.ReloadChannelCache())
			model.CacheUpdateChannelStatus(11, status)
			queries := 0
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("source:no_query", func(*gorm.DB) { queries++ }))
			t.Cleanup(func() { db.Callback().Query().Remove("source:no_query") })
			// Empty keys are valid for this single-key gateway. Source, not key or
			// target status, decides whether automatic changes may use the fast path.
			if status == common.ChannelStatusEnabled {
				EnableChannel(11, "", "automatic recovery")
			} else {
				DisableChannel(types.ChannelError{ChannelId: 11, AutoBan: true}, "automatic disable")
			}
			require.Zero(t, queries)
		})
	}
}
