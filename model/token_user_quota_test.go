package model

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenQuotaQueryCacheRevocation(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := User{Username: "quota-cache-owner"}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "quotaquerycachefixture", UnlimitedQuota: true, AllowUserQuotaQuery: true}
	require.NoError(t, token.Insert())
	_, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.True(t, cached.AllowUserQuotaQuery)
	// Simulate a committed revocation whose Redis invalidation did not arrive.
	require.NoError(t, DB.Model(&token).Update("allow_user_quota_query", false).Error)
	loaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.False(t, loaded.AllowUserQuotaQuery)
	// Pre-upgrade hashes do not opt existing keys into account disclosure.
	require.NoError(t, common.RDB.HDel(context.Background(), getTokenCacheKey(token.Key), "AllowUserQuotaQuery").Err())
	cached, err = cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.False(t, cached.AllowUserQuotaQuery)
}
