package service

import "github.com/gin-gonic/gin"

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
	compact := GetCompactAttemptedKeyIndexes(c, channelID)
	live := GetLiveRouteAttemptedKeyIndexes(c, channelID)
	if len(compact) == 0 && len(live) == 0 {
		return nil
	}
	combined := make(map[int]struct{}, len(compact)+len(live))
	for index := range compact {
		combined[index] = struct{}{}
	}
	for index := range live {
		combined[index] = struct{}{}
	}
	return combined
}
