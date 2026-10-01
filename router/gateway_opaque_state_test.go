package router

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayOpaqueStateHTTP(t *testing.T) {
	for _, test := range []struct{ name, path, first, next, response, identifier string }{
		{"response_item", "/v1/responses", `{"model":"gpt-4o-mini","input":"hello"}`, `{"model":"gpt-4o-mini","input":[{"type":"item_reference","id":"item_state_one"}]}`, `{"id":"resp_state_one","object":"response","status":"completed","output":[{"id":"item_state_one","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`, "item_state_one"},
		{"encrypted_reasoning", "/v1/responses", `{"model":"gpt-4o-mini","input":"hello"}`, `{"model":"gpt-4o-mini","input":[{"type":"reasoning","encrypted_content":"cipher_state_one"}]}`, `{"id":"resp_state_one","object":"response","status":"completed","output":[{"id":"reason_state_one","type":"reasoning","encrypted_content":"cipher_state_one"},{"id":"message_state_one","type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`, "cipher_state_one"},
		{"conversation", "/v1/responses", `{"model":"gpt-4o-mini","input":"hello"}`, `{"model":"gpt-4o-mini","input":"hello","conversation":"conversation_state_one"}`, `{"id":"resp_state_one","object":"response","status":"completed","conversation":{"id":"conversation_state_one"},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`, "conversation_state_one"},
		{"redacted_thinking", "/v1/messages", `{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`, `{"model":"claude-3-5-sonnet","messages":[{"role":"assistant","content":[{"type":"redacted_thinking","data":"redacted_state_one"}]},{"role":"user","content":"continue"}],"max_tokens":100}`, `{"id":"msg_state_one","type":"message","role":"assistant","content":[{"type":"redacted_thinking","data":"redacted_state_one"},{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`, "redacted_state_one"},
		{"thinking_signature", "/v1/messages", `{"model":"claude-3-5-sonnet","messages":[{"role":"user","content":"hello"}],"max_tokens":100}`, `{"model":"claude-3-5-sonnet","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"signature_state_one"}]},{"role":"user","content":"continue"}],"max_tokens":100}`, `{"id":"msg_state_one","type":"message","role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"signature_state_one"},{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":2}}`, "signature_state_one"},
		{"gemini_signature", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`, `{"contents":[{"role":"model","parts":[{"text":"hello","thoughtSignature":"signature_state_one"}]},{"role":"user","parts":[{"text":"continue"}]}]}`, `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello","thoughtSignature":"signature_state_one"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`, "signature_state_one"},
		{"gemini_file_uri", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"parts":[{"text":"hello"}]}]}`, `{"contents":[{"parts":[{"text":"describe"},{"fileData":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/file_state_one"}}]}]}`, `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"},{"fileData":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/file_state_one"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`, "file_state_one"},
		{"gemini_storage_uri", "/v1beta/models/gemini-2.5-flash:generateContent", `{"contents":[{"parts":[{"text":"hello"}]}]}`, `{"contents":[{"parts":[{"text":"describe"},{"fileData":{"mimeType":"image/png","fileUri":"gs://fixture-private/file_state_one"}}]}]}`, `{"candidates":[{"index":0,"content":{"role":"model","parts":[{"text":"hello"},{"fileData":{"mimeType":"image/png","fileUri":"gs://fixture-private/file_state_one"}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":2,"totalTokenCount":12}}`, "file_state_one"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var sends atomic.Int64
			url, key, foreignKey := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				attempt := sends.Add(1)
				if attempt == 2 && (test.name == "gemini_file_uri" || test.name == "gemini_storage_uri") {
					data, err := io.ReadAll(r.Body)
					if !assert.NoError(t, err) || !assert.Contains(t, string(data), test.identifier, "native continuation must send the bound media reference") {
						http.Error(w, "missing media reference", http.StatusBadRequest)
						return
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, test.response)
			})
			status, body := gatewayStateHTTP(t, url, key, test.path, test.first)
			require.Equal(t, 200, status, body)
			require.Contains(t, body, test.identifier)
			status, body = gatewayStateHTTP(t, url, key, test.path, test.next)
			require.Equal(t, 200, status, body)
			assert.EqualValues(t, 2, sends.Load())
			status, body = gatewayStateHTTP(t, url, foreignKey, test.path, test.next)
			assert.Equal(t, 400, status, body)
			status, body = gatewayStateHTTP(t, url, key, test.path, strings.ReplaceAll(test.next, test.identifier, "unknown_state"))
			assert.Equal(t, 400, status, body)
			assert.EqualValues(t, 2, sends.Load(), "unknown and foreign state never reach an upstream")
		})
	}
}

func TestGatewayStateBindingFailureNeverExposesIdentifier(t *testing.T) {
	for _, stream := range []string{"false", "true"} {
		t.Run(stream, func(t *testing.T) {
			var sends atomic.Int64
			url, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
			require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test:fail_state_binding", func(tx *gorm.DB) {
				if tx.Statement.Table == "gateway_state_bindings" {
					tx.AddError(errors.New("isolated binding failure"))
				}
			}))
			var before, after model.User
			require.NoError(t, model.DB.Where("username = ?", "sdk-isolated").First(&before).Error)
			status, body := gatewayStateHTTP(t, url, key, "/v1/responses", `{"model":"gpt-4o-mini","input":"hello","stream":`+stream+`}`)
			assert.Equal(t, 503, status, body)
			assert.NotContains(t, body, "resp_fixture")
			assert.NotContains(t, body, "isolated binding failure", "internal storage errors must not reach SDKs")
			assert.NotContains(t, body, "response.completed")
			assert.EqualValues(t, 1, sends.Load(), "accounting/ownership failure cannot resend the generation")
			// The generation never reached the client, so the reservation is
			// released instead of charged or held.
			require.EventuallyWithT(t, func(collect *assert.CollectT) {
				var recovery model.BillingRecovery
				if assert.NoError(collect, model.DB.First(&after, before.Id).Error) && assert.NoError(collect, model.DB.First(&recovery).Error) {
					assert.Equal(collect, model.BillingRecoveryStatusRefunded, recovery.Status)
					assert.Equal(collect, before.Quota, after.Quota)
					assert.Equal(collect, before.UsedQuota, after.UsedQuota)
					assert.Zero(collect, after.ReservedQuota)
				}
			}, time.Second, 5*time.Millisecond)
		})
	}
}

func gatewayStateHTTP(t *testing.T, base, token, path, body string) (int, string) {
	t.Helper()
	request, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(body))
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := (&http.Client{Timeout: 5 * time.Second}).Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(data)
}

func TestGatewayCompactionStateContinuesResponsesHTTP(t *testing.T) {
	var sends atomic.Int64
	url, key, foreignKey := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		if strings.HasSuffix(r.URL.Path, "/compact") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"compaction_fixture","object":"response.compaction","output":[{"type":"compaction","id":"cmp_local","encrypted_content":"compact_cipher"}],"usage":{"input_tokens":10,"output_tokens":2,"total_tokens":12}}`)
			return
		}
		gatewaySDKUpstream(w, r)
	})
	require.NoError(t, configureGatewayFixturePrices(`{"gpt-4o-mini-openai-compact":0.001}`, false))
	status, body := gatewayStateHTTP(t, url, key, "/v1/responses/compact", `{"model":"gpt-4o-mini","input":[{"role":"user","content":"hello"}]}`)
	require.Equal(t, 200, status, body)
	require.Contains(t, body, "compact_cipher")
	request := `{"model":"gpt-4o-mini","input":[{"type":"compaction","id":"cmp_local","encrypted_content":"compact_cipher"},{"role":"user","content":"hello"}]}`
	status, body = gatewayStateHTTP(t, url, key, "/v1/responses", request)
	require.Equal(t, 200, status, body)
	status, body = gatewayStateHTTP(t, url, foreignKey, "/v1/responses", request)
	assert.Equal(t, 400, status, body)
	assert.EqualValues(t, 2, sends.Load())
}
