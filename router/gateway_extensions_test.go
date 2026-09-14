package router

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayStrictExtensionsRejectedBeforeSendingOrBilling(t *testing.T) {
	for _, test := range []struct{ name, path, body, native, converted string }{
		{"chat", "/v1/chat/completions", `{"model":"%s","messages":[{"role":"user","content":"hello"}],"vendor_flag":false}`, "gpt-4o-mini", "gemini-2.5-flash"},
		{"responses", "/v1/responses", `{"model":"%s","input":"hello","vendor_flag":false}`, "gpt-4o-mini", "gemini-2.5-flash"},
		{"claude", "/v1/messages", `{"model":"%s","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"vendor_flag":false}`, "claude-3-5-sonnet", "gpt-4o-mini"},
		{"gemini", "/v1beta/models/%s:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"vendor_flag":false}`, "gemini-2.5-flash", "gpt-4o-mini"},
	} {
		for _, modelName := range []string{test.native, test.converted} {
			t.Run(test.name+"/"+modelName, func(t *testing.T) {
				var sends atomic.Int64
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
				require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("conversion_policy", "strict").Error)
				var token model.Token
				require.NoError(t, model.DB.Where("key = ?", key).First(&token).Error)
				var before model.User
				require.NoError(t, model.DB.First(&before, token.UserId).Error)
				path, body := test.path, test.body
				if test.name == "gemini" {
					path = fmt.Sprintf(path, modelName)
				} else {
					body = fmt.Sprintf(body, modelName)
				}
				request, err := http.NewRequest(http.MethodPost, url+path, strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Authorization", "Bearer "+key)
				response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				output, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Equal(t, http.StatusBadRequest, response.StatusCode, string(output))
				assert.Zero(t, sends.Load())
				var recoveries int64
				require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
				assert.Zero(t, recoveries, "conversion rejection must happen before any debit")
				var after model.User
				require.NoError(t, model.DB.First(&after, token.UserId).Error)
				assert.Equal(t, before.Quota, after.Quota)
				assert.Equal(t, before.UsedQuota, after.UsedQuota)
			})
		}
	}
}
