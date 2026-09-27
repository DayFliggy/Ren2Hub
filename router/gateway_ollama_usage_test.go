package router

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayOllamaUsagePresenceAccounting(t *testing.T) {
	for _, test := range []struct {
		name, counts string
		known        bool
		charge       common.Quota
	}{
		{"missing", "", false, 0},
		{"null", `,"prompt_eval_count":null,"eval_count":null`, false, 0},
		{"missing_output", `,"prompt_eval_count":5`, false, 0},
		{"missing_input", `,"eval_count":7`, false, 0},
		{"zero", `,"prompt_eval_count":0,"eval_count":0`, true, 0},
		{"complete", `,"prompt_eval_count":5,"eval_count":7`, true, 19},
	} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", test.name, stream), func(t *testing.T) {
				var sends atomic.Int32
				var reservedAtSend atomic.Int64
				base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					assert.Equal(t, "/api/chat", r.URL.Path)
					var user model.User
					if assert.NoError(t, model.DB.First(&user, 1).Error) {
						reservedAtSend.Store(int64(user.ReservedQuota))
						assert.Positive(t, user.ReservedQuota, "reservation must precede the provider request")
					}
					contentType := "application/json"
					if stream {
						contentType = "application/x-ndjson"
					}
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, `{"model":"llama3.1","message":{"role":"assistant","content":"paid answer"},"done":true,"done_reason":"stop"`+test.counts+"}\n")
				})
				current, err := model.GetActiveBillingPrice(context.Background(), "llama3.1")
				require.NoError(t, err)
				_, err = model.PublishBillingPrice(context.Background(), billing.PriceVersion{Provenance: &billing.PriceProvenance{Kind: "retail", RetailReason: "explicit test retail price"}, Model: "llama3.1", Mode: billing.ModeExpression, Expression: "p * 2 + c * 4"}, current.ID, 1)
				require.NoError(t, err)
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 1000000}).Error)
				code, body := gatewayStateHTTP(t, base, key, "/v1/chat/completions", fmt.Sprintf(`{"model":"llama3.1","max_tokens":32,"stream":%t,"messages":[{"role":"user","content":"fixture"}]}`, stream))
				require.Equal(t, http.StatusOK, code, body)
				assert.Contains(t, body, "paid answer")
				awaitGatewayPermitsReleased(t)
				assert.EqualValues(t, 1, sends.Load())
				var recovery model.BillingRecovery
				var user model.User
				var token model.Token
				require.NoError(t, model.DB.First(&recovery).Error)
				require.NoError(t, model.DB.First(&user, 1).Error)
				require.NoError(t, model.DB.First(&token, 1).Error)
				held := common.Quota(reservedAtSend.Load())
				if test.known {
					assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
					assert.Equal(t, model.BillingRecoveryStateCompleted, recovery.ConsumptionState)
					held = 0
				} else {
					assert.Equal(t, model.BillingRecoveryStatusReconciliation, recovery.Status)
					assert.True(t, model.BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState))
					assert.Positive(t, held)
				}
				assert.Equal(t, test.charge, user.UsedQuota)
				assert.Equal(t, held, user.ReservedQuota)
				assert.EqualValues(t, 10000000-test.charge-held, user.Quota)
				assert.Equal(t, test.charge, token.UsedQuota)
				assert.Equal(t, held, token.ReservedQuota)
				assert.EqualValues(t, 1000000-test.charge-held, token.RemainQuota)
				var settled, released int64
				require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", recovery.RequestID, "settle").Count(&settled).Error)
				require.NoError(t, model.DB.Model(&model.BillingEvent{}).Where("request_id = ? AND operation = ?", recovery.RequestID, "release").Count(&released).Error)
				assert.Zero(t, released, "accepted generation must never be automatically released")
				if test.known {
					assert.EqualValues(t, 1, settled)
				} else {
					assert.Zero(t, settled, "unconfirmed usage must not settle, even at zero")
				}
			})
		}
	}
}
