package router

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayGeminiInvalidInputPrecedesStateLookup(t *testing.T) {
	for _, action := range []string{"generateContent", "streamGenerateContent"} {
		for _, body := range []string{`{`, `[]`, `null`, `{}`, `{"contents":"invalid"}`} {
			t.Run(action+"/"+body, func(t *testing.T) {
				var sends atomic.Int32
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1) })
				status, result := gatewayLegacyOperationRequest(t, url, key, "/v1beta/models/gemini-2.5-flash:"+action, body)
				assert.Equal(t, http.StatusBadRequest, status, result)
				assert.NotContains(t, result, "state_unavailable")
				assert.Zero(t, sends.Load())
				var user model.User
				require.NoError(t, model.DB.First(&user, 1).Error)
				assert.Equal(t, 10000000, user.Quota)
				var recoveries, failures int64
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
				assert.Zero(t, recoveries)
				assert.Zero(t, failures)
			})
		}
	}
}
