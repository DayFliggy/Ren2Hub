package router

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayOAuthRefreshPreservesStateHTTP(t *testing.T) {
	for _, changedAccount := range []bool{false, true} {
		t.Run(fmt.Sprintf("changed_account=%t", changedAccount), func(t *testing.T) {
			var sends atomic.Int64
			credentials := make(chan string, 4)
			base, token, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				credentials <- r.Header.Get("Authorization")
				assert.Equal(t, "fixture-account", r.Header.Get("Chatgpt-Account-Id"))
				gatewaySDKUpstream(w, r)
			})
			oldKey, err := common.Marshal(service.CodexOAuthKey{AccessToken: "fixture-access", RefreshToken: "fixture-refresh", AccountID: "fixture-account"})
			require.NoError(t, err)
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("type = ?", constant.ChannelTypeOpenAI).Updates(map[string]any{"type": constant.ChannelTypeCodex, "key": string(oldKey)}).Error)
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			status, body := gatewayStateHTTP(t, base, token, "/v1/responses", `{"model":"gpt-4o-mini","input":"hello"}`)
			require.Equal(t, 200, status, body)
			assert.Equal(t, "Bearer fixture-access", <-credentials)
			var binding model.GatewayStateBinding
			require.NoError(t, model.DB.Where("kind = ?", "response").First(&binding).Error)
			account := "fixture-account"
			if changedAccount {
				account = "different-account"
			}
			claims, err := common.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account}})
			require.NoError(t, err)
			access := "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".fixture"
			refreshServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.NoError(t, r.ParseForm())
				assert.Equal(t, "refresh_token", r.Form.Get("grant_type"))
				assert.Equal(t, "fixture-refresh", r.Form.Get("refresh_token"))
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"access_token":%q,"refresh_token":"fixture-next-refresh","expires_in":3600}`, access)
			}))
			t.Cleanup(refreshServer.Close)
			refreshURL, err := url.Parse(refreshServer.URL)
			require.NoError(t, err)
			client := service.GetHttpClient()
			previousTransport := client.Transport
			client.Transport = gatewayPromptTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "auth.openai.com" && r.URL.Path == "/oauth/token" {
					forward := r.Clone(r.Context())
					forward.URL.Scheme, forward.URL.Host, forward.Host = refreshURL.Scheme, refreshURL.Host, refreshURL.Host
					return previousTransport.RoundTrip(forward)
				}
				if r.URL.Hostname() != "127.0.0.1" {
					return nil, errors.New("external requests are disabled in the OAuth fixture")
				}
				return previousTransport.RoundTrip(r)
			})
			t.Cleanup(func() { client.Transport = previousTransport })
			_, refreshed, refreshErr := service.RefreshCodexChannelCredential(context.Background(), binding.ChannelID, service.CodexCredentialRefreshOptions{})
			if changedAccount {
				require.ErrorContains(t, refreshErr, "different account")
				var stored model.Channel
				require.NoError(t, model.DB.First(&stored, binding.ChannelID).Error)
				assert.Equal(t, string(oldKey), stored.Key)
				assert.EqualValues(t, 1, sends.Load())
				return
			}
			require.NoError(t, refreshErr)
			identity, err := model.EnsureGatewayCredential(context.Background(), refreshed.Id, refreshed.Key)
			require.NoError(t, err)
			assert.Equal(t, binding.CredentialID, identity.ID)
			status, body = gatewayStateHTTP(t, base, token, "/v1/responses", `{"model":"gpt-4o-mini","input":"hello","previous_response_id":"resp_fixture"}`)
			require.Equal(t, 200, status, body)
			assert.Equal(t, "Bearer "+access, <-credentials)
			assert.EqualValues(t, 2, sends.Load())
			replacement, err := common.Marshal(service.CodexOAuthKey{AccessToken: "manual-replacement", AccountID: "fixture-account"})
			require.NoError(t, err)
			refreshed.Key = string(replacement)
			require.NoError(t, refreshed.Update())
			status, body = gatewayStateHTTP(t, base, token, "/v1/responses", `{"model":"gpt-4o-mini","input":"hello","previous_response_id":"resp_fixture"}`)
			assert.Equal(t, 503, status, body)
			assert.EqualValues(t, 2, sends.Load())
		})
	}
}
