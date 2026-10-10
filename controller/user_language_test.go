package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The account language selects the language of backend messages, emails and
// notifications. Only en, zh-CN and zh-TW are saved, aliases in their canonical
// form, and the cached user that TokenAuth reads follows at once.
func TestUpdateSelfSavesOnlySupportedLanguages(t *testing.T) {
	user, identity := setupSecurityEnrollmentTest(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	previousRDB := common.RDB
	common.RDB, common.RedisEnabled = client, true
	t.Cleanup(func() {
		common.RDB, common.RedisEnabled = previousRDB, false
		require.NoError(t, client.Close())
	})
	cached, err := model.GetUserCache(user.Id)
	require.NoError(t, err)
	require.Empty(t, cached.GetSetting().Language)

	for _, tc := range []struct {
		body   string
		status int
		want   string
	}{
		{`{"language":"zhTW"}`, http.StatusOK, "zh-TW"},
		{`{"language":"en-US"}`, http.StatusOK, "en"},
		{`{"language":"zh_CN"}`, http.StatusOK, "zh-CN"},
		{`{"language":"fr"}`, http.StatusBadRequest, "zh-CN"},
		{`{"language":""}`, http.StatusBadRequest, "zh-CN"},
		{`{"language":7}`, http.StatusBadRequest, "zh-CN"},
	} {
		response := securityEnrollmentRequest(http.MethodPut, "/api/user/self", tc.body, "", identity, UpdateSelf)
		assert.Equal(t, tc.status, response.Code, tc.body)
		if tc.status == http.StatusBadRequest {
			assert.JSONEq(t, `{"success":false,"message":"Unsupported language","message_key":"Unsupported language"}`, response.Body.String(), tc.body)
		}
		stored, err := model.GetUserById(user.Id, true)
		require.NoError(t, err)
		assert.Equal(t, tc.want, stored.GetSetting().Language, tc.body)
		cached, err := model.GetUserCache(user.Id)
		require.NoError(t, err)
		assert.Equal(t, tc.want, cached.GetSetting().Language, tc.body)
	}
}
