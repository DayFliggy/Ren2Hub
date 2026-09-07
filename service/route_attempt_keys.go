package service

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func RequestRouteAttemptedKeys(c *gin.Context, stage relaycommon.CompactAttemptStage) map[int]map[int]struct{} {
	if stage != relaycommon.CompactAttemptNone {
		value, _ := c.Get(compactAttemptedKeysContextKey)
		attempted, _ := value.(CompactAttemptedKeyIndexes)
		return attempted
	}
	value, _ := c.Get(liveRouteAttemptedKeysContextKey)
	attempted, _ := value.(LiveRouteAttemptedKeyIndexes)
	return attempted
}

const liveRouteAttemptedKeysContextKey = "live_route_attempted_key_indexes"

type LiveRouteAttemptedKeyIndexes map[int]map[int]struct{}

func RecordLiveRouteAttemptedKey(c *gin.Context, channelID, keyIndex int) {
	if c == nil || channelID <= 0 || keyIndex < 0 {
		return
	}
	attempted := LiveRouteAttemptedKeyIndexes{}
	if value, ok := c.Get(liveRouteAttemptedKeysContextKey); ok {
		if existing, valid := value.(LiveRouteAttemptedKeyIndexes); valid {
			attempted = existing
		}
	}
	if attempted[channelID] == nil {
		attempted[channelID] = make(map[int]struct{})
	}
	attempted[channelID][keyIndex] = struct{}{}
	c.Set(liveRouteAttemptedKeysContextKey, attempted)
}

func GetLiveRouteAttemptedKeyIndexes(c *gin.Context, channelID int) map[int]struct{} {
	if c == nil || channelID <= 0 {
		return nil
	}
	value, ok := c.Get(liveRouteAttemptedKeysContextKey)
	if !ok {
		return nil
	}
	attempted, ok := value.(LiveRouteAttemptedKeyIndexes)
	if !ok {
		return nil
	}
	return attempted[channelID]
}

func GetRouteAttemptedKeyIndexes(c *gin.Context, channelID int) map[int]struct{} {
	if CompactStageFromContext(c) != relaycommon.CompactAttemptNone {
		return GetCompactAttemptedKeyIndexes(c, channelID)
	}
	return GetLiveRouteAttemptedKeyIndexes(c, channelID)
}
