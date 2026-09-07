package service

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRouteAttemptedKeysAreIsolatedByCompactStage(t *testing.T) {
	c, _ := gin.CreateTestContext(nil)
	SetCompactAttemptedKeyIndexes(c, CompactAttemptedKeyIndexes{7: {1: {}}})
	RecordLiveRouteAttemptedKey(c, 7, 2)
	RecordLiveRouteAttemptedKey(c, 7, 2)

	assert.Equal(t, map[int]struct{}{2: {}}, GetRouteAttemptedKeyIndexes(c, 7))
	SetCompactStage(c, relaycommon.CompactAttemptExact)
	assert.Equal(t, map[int]struct{}{1: {}}, GetRouteAttemptedKeyIndexes(c, 7))
	SetCompactStage(c, relaycommon.CompactAttemptBase)
	SetCompactAttemptedKeyIndexes(c, CompactAttemptedKeyIndexes{})
	assert.Empty(t, GetRouteAttemptedKeyIndexes(c, 7))
	assert.Empty(t, GetRouteAttemptedKeyIndexes(c, 8))
}
