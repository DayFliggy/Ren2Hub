package router

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Playground has a browser session and an ephemeral model token. The logical
// model must still be initialized before routing and survive provider retries.
func TestGatewayPlaygroundModelIdentityWithoutPersistentToken(t *testing.T) {
	const logical = "gpt-4o-mini"
	for _, failFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("failover=%t", failFirst), func(t *testing.T) {
			var mu sync.Mutex
			var sentModels []string
			base, _, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/chat/completions", r.URL.Path)
				var body map[string]any
				if !assert.NoError(t, common.DecodeJson(r.Body, &body)) {
					http.Error(w, "invalid test payload", http.StatusBadRequest)
					return
				}
				mapped, ok := body["model"].(string)
				assert.True(t, ok)
				mu.Lock()
				sentModels = append(sentModels, mapped)
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				if r.Header.Get("Authorization") == "Bearer local-test-key" && failFirst {
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"error":{"code":"model_not_found","type":"invalid_request_error","message":"mapped deployment is unavailable"}}`)
					return
				}
				_, _ = fmt.Fprintf(w, `{"id":"pg-response","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"playground answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`, "returned-"+mapped)
			})
			user := &model.User{Username: "playground-browser", AffCode: "pg-browser", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 10000000, Group: "default"}
			require.NoError(t, model.DB.Create(user).Error)
			access := createRouterBrowserSession(t, user.Id)
			for id, mapped := range map[int]string{1: "upstream-first", 4: "upstream-second"} {
				channel, err := model.GetChannelById(id, true)
				require.NoError(t, err)
				mapping := fmt.Sprintf(`{%q:%q}`, logical, mapped)
				channel.ModelMapping = &mapping
				require.NoError(t, model.DB.Model(channel).Update("model_mapping", mapping).Error)
				// Different prices make accidental use of wire/returned model
				// identity detectable even if every name happens to be priced.
				require.NoError(t, setGatewayModelPrice(mapped, model.PricingValues{"ModelPrice": "9"}))
				require.NoError(t, setGatewayModelPrice("returned-"+mapped, model.PricingValues{"ModelPrice": "17"}))
			}
			require.NoError(t, service.InitRouteCapabilityIndex(t.Context()))
			var tokens int64
			require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Count(&tokens).Error)
			require.Zero(t, tokens)
			events := captureGatewayRouteDecisions(t)
			request, err := http.NewRequest(http.MethodPost, base+"/pg/chat/completions", strings.NewReader(`{"model":"gpt-4o-mini","messages":[{"role":"user","content":"browser-only request"}],"max_tokens":16}`))
			require.NoError(t, err)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+access)
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			payload, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, http.StatusOK, response.StatusCode, string(payload))
			decision := awaitGatewayRouteDecision(t, events, response.Header.Get(common.RequestIdKey))
			wantModels := []string{"upstream-first"}
			if failFirst {
				wantModels = append(wantModels, "upstream-second")
			}
			mu.Lock()
			actualModels := append([]string(nil), sentModels...)
			mu.Unlock()
			assert.Equal(t, wantModels, actualModels, "each channel maps from the logical model")
			assert.Equal(t, logical, decision.RequestModel)
			assert.Equal(t, user.Id, decision.UserID)
			assert.Zero(t, decision.TokenID)
			require.Len(t, decision.Attempts, len(wantModels))
			assert.Equal(t, failFirst, decision.FailoverAttempt == 1)
			finalWireModel := wantModels[len(wantModels)-1]
			assert.Contains(t, string(payload), `"model":"returned-`+finalWireModel+`"`)

			var recovery model.BillingRecovery
			require.NoError(t, model.DB.Where("request_id = ?", decision.RequestID).First(&recovery).Error)
			assert.Equal(t, model.BillingRecoveryStatusSettled, recovery.Status)
			assert.Zero(t, recovery.TokenID)
			assert.False(t, recovery.TokenRequired)
			var attempts []model.BillingAttempt
			require.NoError(t, model.DB.Where("request_id = ?", decision.RequestID).Find(&attempts).Error)
			require.Len(t, attempts, len(wantModels))
			for _, attempt := range attempts {
				var quote billing.Quote
				require.NoError(t, common.UnmarshalJsonStr(attempt.Quote, &quote))
				assert.Equal(t, logical, quote.PriceVersion.Model)
				assert.Equal(t, "0.001", fmt.Sprint(quote.PriceVersion.Pricing["ModelPrice"]))
			}
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("request_id = ? AND type = ?", decision.RequestID, model.LogTypeConsume).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, logical, logs[0].ModelName)
			assert.Zero(t, logs[0].TokenId)
			var other map[string]any
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
			assert.Equal(t, finalWireModel, other["upstream_model_name"])
			assert.Equal(t, "returned-"+finalWireModel, other["response_model_name"])
			require.NoError(t, model.DB.First(user, user.Id).Error)
			assert.EqualValues(t, 500, user.UsedQuota, "wire and returned identities never select pricing")
			assert.Zero(t, user.ReservedQuota)
			require.NoError(t, model.DB.Model(&model.Token{}).Where("user_id = ?", user.Id).Count(&tokens).Error)
			assert.Zero(t, tokens, "Playground must not insert a persistent model token")

			// A mapped provider name is not itself a user-visible routing model.
			code, body := postGatewayTask(t, base, access, "/pg/chat/completions", fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"cannot bypass logical model admission"}]}`, finalWireModel))
			assert.Equal(t, http.StatusServiceUnavailable, code, body)
			mu.Lock()
			assert.Len(t, sentModels, len(wantModels))
			mu.Unlock()
			assertGatewayRouteIdle(t, 1, 4)
		})
	}
}
