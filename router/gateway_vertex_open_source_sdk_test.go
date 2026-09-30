package router

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type gatewayVertexOpenSourceFixture struct {
	base, token string
	channels    []*model.Channel
	config      []map[string]any
	keys        map[string]*rsa.PrivateKey
	sends       map[string]int
	authCalls   map[string]int
	lock        sync.Mutex
}

func (fixture *gatewayVertexOpenSourceFixture) credential(t *testing.T, project string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	issuer := project + "@local.invalid"
	fixture.keys[issuer] = key
	credential, err := common.Marshal(map[string]string{"project_id": project, "client_email": issuer, "private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}))})
	require.NoError(t, err)
	return string(credential)
}

func setupGatewayVertexOpenSource(t *testing.T) *gatewayVertexOpenSourceFixture {
	t.Helper()
	base, token, _ := setupGatewaySDK(t)
	fixture := &gatewayVertexOpenSourceFixture{base: base, token: token, keys: map[string]*rsa.PrivateKey{}, sends: map[string]int{}, authCalls: map[string]int{}}
	require.NoError(t, model.DB.Model(&model.Channel{}).Where("id > 0").Update("status", common.ChannelStatusManuallyDisabled).Error)
	prices := map[string]float64{}
	for _, domain := range []struct{ name, upstream string }{{"llama", "meta/llama-3.3-70b-instruct"}, {"maas", "deepseek-ai/deepseek-r1-maas"}} {
		credential := fixture.credential(t, "vertex-"+domain.name)
		for _, mode := range []string{"json", "api_key"} {
			alias, key := "vertex-"+domain.name+"-"+mode, credential
			if mode == "api_key" {
				key = "isolated-unsupported-vertex-key"
			}
			mapping, err := common.Marshal(map[string]string{alias: domain.upstream})
			require.NoError(t, err)
			mappingJSON, settings, endpoint := string(mapping), fmt.Sprintf(`{"vertex_key_type":%q}`, mode), "https://vertex-open-source.invalid"
			channel := &model.Channel{Type: constant.ChannelTypeVertexAi, Name: "local Vertex open source", Models: alias, Key: key,
				BaseURL: &endpoint, ModelMapping: &mappingJSON, OtherSettings: settings, Other: "global", Status: common.ChannelStatusEnabled, Group: "default", CapacityTotal: 10, ChannelRatio: "1"}
			require.NoError(t, model.DB.Create(channel).Error)
			require.NoError(t, channel.AddAbilities(model.DB))
			fixture.channels = append(fixture.channels, channel)
			fixture.config = append(fixture.config, map[string]any{"name": alias, "model": alias,
				"supported": map[string]bool{"chat": mode == "json", "responses": mode == "json", "anthropic": false, "gemini": mode == "json"}})
			prices[alias] = 0.001
		}
	}
	priceJSON, err := common.Marshal(prices)
	require.NoError(t, err)
	require.NoError(t, configureGatewayFixturePrices(string(priceJSON), false))
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	client := service.GetHttpClient()
	original := client.Transport
	t.Cleanup(func() { client.Transport = original })
	client.Transport = gatewayPromptTransport(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "www.googleapis.com" && request.URL.Path == "/oauth2/v4/token" {
			if err := request.ParseForm(); err != nil {
				return nil, err
			}
			assert.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", request.Form.Get("grant_type"))
			var issuer string
			_, err := jwt.Parse(request.Form.Get("assertion"), func(token *jwt.Token) (any, error) {
				var err error
				issuer, err = token.Claims.GetIssuer()
				if err != nil || fixture.keys[issuer] == nil {
					return nil, fmt.Errorf("unknown local JWT issuer")
				}
				return &fixture.keys[issuer].PublicKey, nil
			}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithAudience("https://www.googleapis.com/oauth2/v4/token"))
			if err != nil {
				return nil, err
			}
			fixture.lock.Lock()
			fixture.authCalls[issuer]++
			fixture.lock.Unlock()
			project := strings.TrimSuffix(issuer, "@local.invalid")
			body := fmt.Sprintf(`{"access_token":"local-token-%s","expires_in":3600}`, project)
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
		}
		if !assert.Equal(t, "vertex-open-source.invalid", request.URL.Host, "all external destinations are blocked") {
			return nil, fmt.Errorf("unexpected Vertex fixture destination")
		}
		body, err := io.ReadAll(request.Body)
		if err != nil {
			return nil, err
		}
		label := gjson.GetBytes(body, "messages.0.content").String()
		fixture.lock.Lock()
		fixture.sends[label]++
		fixture.lock.Unlock()
		project := request.Header.Get("X-Goog-User-Project")
		valid := assert.Equal(t, "Bearer local-token-"+project, request.Header.Get("Authorization"))
		valid = assert.Equal(t, "/v1beta1/projects/"+project+"/locations/global/endpoints/openapi/chat/completions", request.URL.Path) && valid
		valid = assert.Contains(t, []string{"meta/llama-3.3-70b-instruct", "deepseek-ai/deepseek-r1-maas"}, gjson.GetBytes(body, "model").String()) && valid
		valid = assert.EqualValues(t, 32, gjson.GetBytes(body, "max_tokens").Int()) && valid
		if !valid {
			return nil, fmt.Errorf("Vertex credential or request mapping mismatch")
		}
		response := httptest.NewRecorder()
		forward := request.Clone(request.Context())
		forward.URL.Path, forward.Body = "/v1/chat/completions", io.NopCloser(bytes.NewReader(body))
		gatewaySDKUpstream(response, forward)
		return response.Result(), nil
	})
	return fixture
}

func TestGatewayOfficialVertexOpenSourceSDK(t *testing.T) {
	python, node := os.Getenv("GATEWAY_SDK_PYTHON"), os.Getenv("GATEWAY_SDK_NODE")
	if python == "" || node == "" {
		t.Skip("fixed SDK runtimes are required")
	}
	for _, runtime := range []struct{ binary, script string }{{python, "verify_compatible.py"}, {node, "verify_compatible.mjs"}} {
		t.Run(runtime.script, func(t *testing.T) {
			fixture := setupGatewayVertexOpenSource(t)
			config, err := common.Marshal(fixture.config)
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, runtime.binary, filepath.Join("..", "tests", "gateway-sdk", runtime.script))
			command.Env = append(os.Environ(), "GATEWAY_SDK_URL="+fixture.base, "GATEWAY_SDK_KEY="+fixture.token, "GATEWAY_COMPATIBLE_CONFIG="+string(config))
			output, err := command.CombinedOutput()
			require.NoError(t, err, "%s", output)
			t.Log(string(output))
			fixture.lock.Lock()
			defer fixture.lock.Unlock()
			for _, channel := range fixture.channels {
				for _, protocol := range []string{"chat", "responses", "anthropic", "gemini"} {
					want := 0
					if channel.GetOtherSettings().VertexKeyType != "api_key" && protocol != "anthropic" {
						want = 1
					}
					for _, mode := range []string{"json", "stream"} {
						assert.Equal(t, want, fixture.sends[channel.Models+"-"+protocol+"-"+mode])
					}
				}
			}
			assert.Len(t, fixture.sends, 12)
			assert.Equal(t, map[string]int{"vertex-llama@local.invalid": 1, "vertex-maas@local.invalid": 1}, fixture.authCalls)
		})
	}
}

func TestGatewayVertexOpenSourceAPIKeyRejectedBeforePreparation(t *testing.T) {
	fixture := setupGatewayVertexOpenSource(t)
	for _, channel := range fixture.channels {
		if channel.GetOtherSettings().VertexKeyType != "api_key" {
			continue
		}
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"blocked"}],"max_tokens":32}`, channel.Models)
		code, response := postGatewayTask(t, fixture.base, fixture.token, "/v1/chat/completions", body)
		assert.Equal(t, http.StatusServiceUnavailable, code, response)
		config := model.DefaultGatewayConfig()
		config.ChannelRates[channel.Id] = model.GatewayProviderRateLimit{TPM: 100}
		encoded, err := common.Marshal(model.DefaultGatewayConfig())
		require.NoError(t, err)
		require.NoError(t, model.DB.Where("key = ?", model.GatewayConfigKey).Delete(&model.Option{}).Error)
		require.NoError(t, model.DB.Create(&model.Option{Key: model.GatewayConfigKey, Value: string(encoded)}).Error)
		assert.ErrorIs(t, model.SaveGatewayConfig(config), model.ErrGatewayTPMUnsupported)
	}
	assert.Empty(t, fixture.sends)
	assert.Empty(t, fixture.authCalls)
	var reservations int64
	require.NoError(t, model.DB.Model(&model.BillingRecovery{}).Count(&reservations).Error)
	assert.Zero(t, reservations)
}

func TestGatewayVertexCredentialCacheFollowsIdentity(t *testing.T) {
	fixture := setupGatewayVertexOpenSource(t)
	channel := fixture.channels[0]
	first, second := channel.Key, fixture.channels[2].Key
	third := fixture.credential(t, "vertex-replacement")
	for index, state := range []struct {
		key  string
		info model.ChannelInfo
	}{
		{first, model.ChannelInfo{}},
		{first + "\n" + second, model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled}}},
		{second + "\n" + first, model.ChannelInfo{IsMultiKey: true, MultiKeySize: 2, MultiKeyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled}}},
		{third, model.ChannelInfo{}},
	} {
		channel.Key, channel.ChannelInfo = state.key, state.info
		require.NoError(t, model.DB.Model(channel).Updates(map[string]any{"key": state.key, "channel_info": state.info}).Error)
		body := fmt.Sprintf(`{"model":%q,"messages":[{"role":"user","content":"rotation-%d"}],"max_tokens":32}`, channel.Models, index)
		code, response := postGatewayTask(t, fixture.base, fixture.token, "/v1/chat/completions", body)
		require.Equal(t, http.StatusOK, code, response)
	}
	assert.Len(t, fixture.sends, 4)
	assert.Equal(t, map[string]int{"vertex-llama@local.invalid": 1, "vertex-maas@local.invalid": 1, "vertex-replacement@local.invalid": 1}, fixture.authCalls)
}
