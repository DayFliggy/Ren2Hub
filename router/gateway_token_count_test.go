package router

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayUnsupportedTokenCountNeverGenerates(t *testing.T) {
	for _, path := range []string{"/v1/messages/count_tokens", "/v1/models/gemini-2.5-flash:countTokens", "/v1beta/models/gemini-2.5-flash:countTokens"} {
		t.Run(path, func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				gatewaySDKUpstream(w, r)
			})
			status, body := gatewayStateHTTP(t, base, key, path, `{"model":"gemini-2.5-flash","contents":[{"parts":[{"text":"count only"}]}]}`)
			assert.Equal(t, http.StatusNotFound, status, body)
			status, body = gatewayStateHTTP(t, base, "", path, `{"contents":[{"parts":[{"text":"count only"}]}]}`)
			assert.Equal(t, http.StatusNotFound, status, body)
			assert.Zero(t, sends.Load(), "token counting must never become a paid generation")
			var count int64
			require.NoError(t, model.DB.Model(&model.BillingAttempt{}).Count(&count).Error)
			assert.Zero(t, count)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.Zero(t, user.UsedQuota)
			assert.Zero(t, user.ReservedQuota)
		})
	}
}
