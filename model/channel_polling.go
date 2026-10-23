package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
)

// advanceChannelPolling is called with the per-channel polling lock held.
// Route candidates are database snapshots, not necessarily the cache-owned
// channel. Advance the authoritative cursor, never the request's snapshot.
func advanceChannelPolling(id, keyCount int, enabled []int) (int, error) {
	var owner *Channel
	if common.MemoryCacheEnabled {
		// The per-channel mutex serializes cursor writes. This read lock only
		// prevents cache replacement; unrelated channels can advance in parallel.
		channelSyncLock.RLock()
		defer channelSyncLock.RUnlock()
		owner = channelsIDM[id]
		if owner == nil {
			return 0, fmt.Errorf("channel #%d no longer exists", id)
		}
	} else {
		var err error
		owner, err = GetChannelById(id, true)
		if err != nil {
			return 0, err
		}
	}
	start := owner.ChannelInfo.MultiKeyPollingIndex
	if start < 0 || start >= keyCount {
		start = 0
	}
	index := enabled[0]
	for _, candidate := range enabled {
		if candidate >= start {
			index = candidate
			break
		}
	}
	owner.ChannelInfo.MultiKeyPollingIndex = (index + 1) % keyCount
	if !common.MemoryCacheEnabled {
		if err := owner.SaveChannelInfo(); err != nil {
			// The cursor is a fairness hint only; a persistence failure must not
			// turn an otherwise valid relay request into a transport failure.
			common.SysLog(common.LogText("failed to persist channel polling cursor: channel_id=%d, error=%v", id, err))
		}
	}
	return index, nil
}
