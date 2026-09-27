package router

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayOfficialCodexSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("GATEWAY_SDK_PYTHON and GATEWAY_SDK_NODE must point to fixed SDK runtimes")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_codex.py"}, {node, "verify_codex.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			base, token, otherToken := setupGatewaySDK(t)
			credential := func(access, account string) string {
				encoded, err := common.Marshal(service.CodexOAuthKey{AccessToken: access, RefreshToken: "fixture-refresh", AccountID: account})
				require.NoError(t, err)
				return string(encoded)
			}
			primaryKey := credential("fixture-access", "fixture-account")
			spareKey := credential("fixture-spare-access", "fixture-spare-account")
			endpoint := "https://codex-fixture.invalid"
			mapping := `{"codex-fixture":"gpt-5-codex","codex-errors":"gpt-5-codex"}`
			for _, id := range []int{1, 4} {
				channel, err := model.GetChannelById(id, true)
				require.NoError(t, err)
				channel.Type, channel.Models, channel.Key, channel.BaseURL, channel.ModelMapping = constant.ChannelTypeCodex, "codex-fixture", primaryKey, &endpoint, &mapping
				if id == 4 {
					channel.Key = spareKey
				}
				require.NoError(t, model.DB.Save(channel).Error)
				require.NoError(t, channel.AddAbilities(model.DB))
			}
			errorChannel := &model.Channel{Type: constant.ChannelTypeCodex, Models: "codex-errors", Key: credential("fixture-error-access", "fixture-error-account"),
				BaseURL: &endpoint, ModelMapping: &mapping, Status: common.ChannelStatusEnabled, Group: "default", ChannelRatio: "1", CapacityTotal: 10}
			require.NoError(t, model.DB.Create(errorChannel).Error)
			require.NoError(t, errorChannel.AddAbilities(model.DB))
			require.NoError(t, publishGatewayFixturePrices(`{"codex-fixture":0.001,"codex-errors":0.001}`, false))
			require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			client := service.GetHttpClient()
			previousTransport := client.Transport
			var lock sync.Mutex
			counts := make(map[string]int)
			client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != "codex-fixture.invalid" {
					assert.Fail(t, "unexpected transport destination", "host=%s", request.URL.Host)
					return nil, errors.New("all external and OAuth network requests are blocked by the Codex SDK fixture")
				}
				body, err := io.ReadAll(request.Body)
				if err != nil {
					return nil, err
				}
				label := gjson.GetBytes(body, "input").String()
				lock.Lock()
				counts[label]++
				lock.Unlock()
				valid := assert.Equal(t, "/backend-api/codex/responses", request.URL.Path)
				valid = assert.Equal(t, "gpt-5-codex", gjson.GetBytes(body, "model").String()) && valid
				valid = assert.Equal(t, "false", gjson.GetBytes(body, "store").Raw) && valid
				valid = assert.Equal(t, `""`, gjson.GetBytes(body, "instructions").Raw) && valid
				valid = assert.False(t, gjson.GetBytes(body, "max_output_tokens").Exists()) && valid
				valid = assert.Equal(t, "application/json", request.Header.Get("Content-Type")) && valid
				valid = assert.Equal(t, "responses=experimental", request.Header.Get("OpenAI-Beta")) && valid
				expectedAccess, expectedAccount := "fixture-access", "fixture-account"
				if strings.HasPrefix(label, "codex-error-") {
					expectedAccess, expectedAccount = "fixture-error-access", "fixture-error-account"
				}
				valid = assert.Equal(t, "Bearer "+expectedAccess, request.Header.Get("Authorization")) && valid
				valid = assert.Equal(t, expectedAccount, request.Header.Get("Chatgpt-Account-Id")) && valid
				if strings.HasPrefix(label, "codex-continue-") {
					valid = assert.Equal(t, "resp_codex-basic-json", gjson.GetBytes(body, "previous_response_id").String()) && valid
					var channel model.Channel
					valid = assert.NoError(t, model.DB.First(&channel, 1).Error) && valid
					valid = assert.Equal(t, spareKey+"\n"+primaryKey, channel.Key) && valid
				}
				if !valid {
					return nil, errors.New("invalid Codex adapter request")
				}
				response := httptest.NewRecorder()
				if label == "codex-error-rejected" {
					response.Header().Set("Content-Type", "application/json")
					response.WriteHeader(http.StatusBadRequest)
					_, _ = io.WriteString(response, `{"error":{"type":"invalid_request_error","code":"invalid_prompt","message":"isolated provider rejection"}}`)
				} else {
					forward := request.Clone(request.Context())
					forward.URL.Path = "/v1/responses"
					forward.Body = io.NopCloser(bytes.NewReader(body))
					gatewaySDKUpstream(response, forward)
				}
				result := response.Result()
				data := bytes.ReplaceAll(response.Body.Bytes(), []byte("resp_fixture"), []byte("resp_"+label))
				data = bytes.ReplaceAll(data, []byte("msg_response_fixture"), []byte("msg_"+label))
				result.Body, result.ContentLength = io.NopCloser(bytes.NewReader(data)), int64(len(data))
				return result, nil
			})
			t.Cleanup(func() { client.Transport = previousTransport })
			control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost {
					w.WriteHeader(http.StatusMethodNotAllowed)
					return
				}
				var binding model.GatewayStateBinding
				if err := model.DB.Where("kind = ?", "response").First(&binding).Error; err != nil {
					http.Error(w, "state was not saved before exposing the response", http.StatusInternalServerError)
					return
				}
				updates := map[string]any{}
				switch request.URL.Path {
				case "/reorder":
					updates["key"] = spareKey + "\n" + primaryKey
					updates["channel_info"] = model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2}
				case "/disable":
					updates["status"] = common.ChannelStatusManuallyDisabled
				case "/replace":
					updates["status"] = common.ChannelStatusEnabled
					updates["key"] = spareKey
					updates["channel_info"] = model.ChannelInfo{}
				default:
					http.NotFound(w, request)
					return
				}
				if err := model.DB.Model(&model.Channel{}).Where("id = ?", binding.ChannelID).Updates(updates).Error; err != nil {
					http.Error(w, "cannot update isolated credential", http.StatusInternalServerError)
					return
				}
				if err := service.InitRouteCapabilityIndex(context.Background()); err != nil {
					http.Error(w, "cannot refresh isolated capabilities", http.StatusInternalServerError)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			t.Cleanup(control.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+base, "GATEWAY_SDK_KEY="+token, "GATEWAY_SDK_FOREIGN_KEY="+otherToken, "GATEWAY_CODEX_CONTROL="+control.URL)
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Log(string(output))
			for _, label := range []string{"codex-basic-json", "codex-basic-stream", "codex-continue-json", "codex-continue-stream", "codex-error-rejected", "codex-error-interrupt"} {
				assert.Equal(t, 1, counts[label], label)
			}
			assert.Len(t, counts, 6, fmt.Sprint(counts))
		})
	}
}
