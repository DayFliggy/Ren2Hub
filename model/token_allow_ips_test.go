package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Saving token settings with an empty allow_ips must lift the restriction in
// the database and in the Redis token cache (AxonHub 95fc2df). The Vue key
// form sends allow_ips as the trimmed textarea value, so clearing it sends "".
func TestTokenSettingsUpdateClearsAllowIps(t *testing.T) {
	truncateTables(t)
	server := useUserCacheMiniRedis(t)
	user := User{Username: "allow-ips-clear", Quota: 100}
	require.NoError(t, DB.Create(&user).Error)
	restricted := "192.0.2.10\n10.0.0.0/8"
	token := Token{UserId: user.Id, Key: "allow-ips-clear-token", Name: "restricted", RemainQuota: 100, AllowIps: &restricted}
	require.NoError(t, token.Insert())
	warmed, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.10", "10.0.0.0/8"}, warmed.GetIpLimits())
	cached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	require.Equal(t, []string{"192.0.2.10", "10.0.0.0/8"}, cached.GetIpLimits())

	empty := ""
	update := Token{Id: token.Id, UserId: user.Id, Name: token.Name, ExpiredTime: token.ExpiredTime, RemainQuota: token.RemainQuota, UnlimitedQuota: token.UnlimitedQuota, AllowIps: &empty}
	require.NoError(t, update.UpdateFromRequest("", nil, false, map[string]bool{"name": true, "expired_time": true, "available_quota": true, "unlimited_quota": true, "allow_ips": true}))
	assert.Empty(t, update.GetIpLimits())

	afterUpdate, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Empty(t, afterUpdate.GetIpLimits(), "the restricted cache entry must not survive the update")
	// Once the mutation fence expires, the next read republishes the cleared list.
	server.FastForward(time.Duration(tokenCacheFenceSeconds+1) * time.Second)
	_, err = GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	recached, err := cacheGetTokenByKey(token.Key)
	require.NoError(t, err)
	assert.Empty(t, recached.GetIpLimits())
}
