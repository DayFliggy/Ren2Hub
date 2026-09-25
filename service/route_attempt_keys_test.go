package service

import (
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRouteAttemptedKeysStayInOneLogicalRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	RecordLiveRouteAttemptedKey(c, 7, 1)
	RecordLiveRouteAttemptedKey(c, 7, 2)
	RecordLiveRouteAttemptedKey(c, 7, 2)
	assert.Equal(t, map[int]struct{}{1: {}, 2: {}}, GetRouteAttemptedKeyIndexes(c, 7))
	assert.Equal(t, map[int]map[int]struct{}{7: {1: {}, 2: {}}}, RequestRouteAttemptedKeys(c))
	assert.Empty(t, GetRouteAttemptedKeyIndexes(c, 8))
}
