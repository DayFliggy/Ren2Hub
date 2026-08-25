package service

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRouteAttemptedKeyIndexesCombineLiveAndCompactScopes(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetCompactAttemptedKeyIndexes(c, CompactAttemptedKeyIndexes{7: {1: {}}})
	RecordLiveRouteAttemptedKey(c, 7, 2)
	RecordLiveRouteAttemptedKey(c, 7, 2)

	combined := GetRouteAttemptedKeyIndexes(c, 7)
	assert.Equal(t, map[int]struct{}{1: {}, 2: {}}, combined)
	assert.Empty(t, GetRouteAttemptedKeyIndexes(c, 8))
}
