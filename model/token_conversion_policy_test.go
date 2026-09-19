package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachedTokenPreservesConversionPolicy(t *testing.T) {
	truncateTables(t)
	useUserCacheMiniRedis(t)
	user := User{Username: "conversion-policy", Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "conversion-policy-token", RemainQuota: 100}
	require.NoError(t, token.Insert())
	require.NoError(t, DB.Model(&Token{}).Where("id = ?", token.Id).Update("conversion_policy", "strict").Error)
	first, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, "strict", first.ConversionPolicy)
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Equal(t, "strict", cached.ConversionPolicy)
	assert.Equal(t, first.RemainQuota, cached.RemainQuota)
}
