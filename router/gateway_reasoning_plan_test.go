package router

import (
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayReasoningAndSearchPolicyBeforeBilling(t *testing.T) {
	for _, test := range []struct{ name, path, body, native, target string }{
		{"claude thinking", "/v1/messages", `{"model":"%s","max_tokens":2048,"messages":[{"role":"user","content":"hello"}],"thinking":{"type":"enabled","budget_tokens":1024}}`, "claude-3-5-sonnet", "gpt-4o-mini"},
		{"gemini thinking zero", "/v1beta/models/%s:generateContent", `{"contents":[{"parts":[{"text":"hello"}]}],"generationConfig":{"thinkingConfig":{"thinkingBudget":0,"includeThoughts":false}}}`, "gemini-2.5-flash", "gpt-4o-mini"},
		{"configured search", "/v1/chat/completions", `{"model":"%s","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"web_search_options":{"search_context_size":"high"}}`, "gpt-4o-mini", "claude-3-5-sonnet"},
		{"chat effort", "/v1/chat/completions", `{"model":"%s","max_tokens":100,"messages":[{"role":"user","content":"hello"}],"reasoning_effort":"high"}`, "gpt-4o-mini", "claude-3-5-sonnet"},
		{"responses effort", "/v1/responses", `{"model":"%s","max_output_tokens":100,"input":"hello","reasoning":{"effort":"high"}}`, "gpt-4o-mini", "gemini-2.5-flash"},
	} {
		for _, policy := range []string{"safe", "strict", "allow"} {
			t.Run(test.name+"/"+policy, func(t *testing.T) {
				var sends atomic.Int64
				url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
				require.NoError(t, model.DB.Model(&model.Token{}).Where("key = ?", key).Update("conversion_policy", policy).Error)
				var token model.Token
				require.NoError(t, model.DB.Where("key = ?", key).First(&token).Error)
				var before, after model.User
				require.NoError(t, model.DB.First(&before, token.UserId).Error)
				for _, target := range []string{test.target, test.native} {
					path, body := test.path, test.body
					if strings.Contains(path, "%s") {
						path = fmt.Sprintf(path, target)
					} else {
						body = fmt.Sprintf(body, target)
					}
					status, output := gatewayStateHTTP(t, url, key, path, body)
					if target == test.target && policy != "allow" {
						assert.Equal(t, http.StatusBadRequest, status, output)
						assert.Zero(t, sends.Load())
						var recoveries int64
						require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&recoveries).Error)
						assert.Zero(t, recoveries)
						require.NoError(t, model.DB.First(&after, token.UserId).Error)
						assert.EqualValues(t, before.Quota, after.Quota)
						continue
					}
					require.Equal(t, http.StatusOK, status, output)
				}
				expected := int64(1)
				if policy == "allow" {
					expected = 2
				}
				assert.Equal(t, expected, sends.Load())
			})
		}
	}
}
