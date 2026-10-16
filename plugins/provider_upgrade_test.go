package plugins_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	vertexcore "github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type providerTokenTransport func(*http.Request) (*http.Response, error)

func (f providerTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// The preceding production revision used these exact sources. Selecting the
// new built-ins must keep a task's stored executable and its completion facts.
func TestProviderFactoryUpgradeKeepsPublishedExecutables(t *testing.T) {
	factoryPriceDatabase(t)
	for _, tc := range []struct {
		key, fixture, sha, version, model, completion string
		facts                                         map[string]any
	}{
		{"google", "pinned-ce505c9b5", "36befb906cf1b38b86416c138d34abb8ddfbc9ca695be925ae34f5567d75e95a", "1.1.0+ren2hub.1", "veo-3.1-generate-preview", `{"name":"operations/old","done":true,"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://media.example/old.mp4"}}]}}}`, nil},
		{"vertex-ai", "pinned-ce505c9b5", "8212612ec4fcc763fd77ed9f1244aebbf437a483efe0f42edc66f26386285976", "1.1.0+ren2hub.1", "veo-3.0-generate-001", `{"name":"projects/p/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/old","done":true,"response":{"videos":[{"bytesBase64Encoded":"AQID","mimeType":"video/mp4"}]}}`, nil},
		{"vidu", "pinned-ce505c9b5", "2093cff746ac59f86feccd161048bbb5eeb92d7e360959eb98cfeb421540fb2f", "1.1.0+ren2hub.1", "viduq2", `{"state":"success","credits":3.500000000000000001}`, map[string]any{"credits": "3.500000000000000001"}},
		{"hailuo", "pinned-59b25959b", "4e084dc4f84f8eb56d48972a1572cb618a653f65b69d1c53e45a7ac6b56e80b3", "1.3.0+ren2hub.1", "MiniMax-H3", `{"task":{"id":"old","status":"succeeded","resolution":"2K","usage":{"output_seconds":5,"input_seconds":7.5,"input_image_count":0}}}`, map[string]any{"resolution": "2K", "seconds": "5", "input_video_seconds": "7.5", "input_images": "0"}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			source, err := os.ReadFile(filepath.Join("testdata", tc.fixture, tc.key+".js"))
			require.NoError(t, err)
			require.Equal(t, tc.sha, fmt.Sprintf("%x", sha256.Sum256(source)))
			previous, err := jsplugin.CompilePlugin(string(source), jsplugin.Options{})
			require.NoError(t, err)
			currentSource, err := builtinplugins.Source(tc.key)
			require.NoError(t, err)
			current, err := jsplugin.CompilePlugin(currentSource, jsplugin.Options{})
			require.NoError(t, err)
			require.Equal(t, tc.version, current.Meta.Version)
			require.NotEqual(t, previous.Meta.Version, current.Meta.Version)
			for _, item := range []struct {
				plugin *jsplugin.LoadedPlugin
				source string
			}{{previous, string(source)}, {current, currentSource}} {
				row := model.TaskPlugin{Key: tc.key, Version: item.plugin.Meta.Version, APIVersion: 1, Enabled: true, Factory: true, Source: model.LongText(item.source), SourceHash: item.plugin.SourceHash}
				require.NoError(t, model.SaveTaskPlugin(&row))
			}
			selected, err := jsplugin.DefaultRegistry.RegisterFactory(currentSource, jsplugin.Options{Key: tc.key})
			require.NoError(t, err)
			assert.Equal(t, current.SourceHash, selected.SourceHash)
			task := &model.Task{Properties: model.Properties{OriginModelName: tc.model, UpstreamModelName: tc.model}, PrivateData: model.TaskPrivateData{PluginRef: &model.TaskPluginRef{Key: tc.key, Version: previous.Meta.Version, SourceHash: previous.SourceHash}}}
			loaded, err := service.LoadTaskPluginForTask(task)
			require.NoError(t, err)
			assert.Equal(t, previous.SourceHash, loaded.SourceHash)
			adaptor := taskplugin.New(loaded)
			if tc.key == "vertex-ai" {
				key, err := rsa.GenerateKey(rand.Reader, 2048)
				require.NoError(t, err)
				der, err := x509.MarshalPKCS8PrivateKey(key)
				require.NoError(t, err)
				credentials, err := common.Marshal(vertexcore.Credentials{ProjectID: "old-project", ClientEmail: "old-source@example.test", PrivateKey: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))})
				require.NoError(t, err)
				service.InitHttpClient()
				client, err := service.GetHttpClientWithProxy("")
				require.NoError(t, err)
				transport := client.Transport
				t.Cleanup(func() { client.Transport = transport })
				exchanges := 0
				client.Transport = providerTokenTransport(func(r *http.Request) (*http.Response, error) {
					exchanges++
					require.Equal(t, "https://www.googleapis.com/oauth2/v4/token", r.URL.String())
					require.NoError(t, r.ParseForm())
					require.Equal(t, "urn:ietf:params:oauth:grant-type:jwt-bearer", r.FormValue("grant_type"))
					token, err := jwt.Parse(r.FormValue("assertion"), func(*jwt.Token) (any, error) { return &key.PublicKey, nil }, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("old-source@example.test"))
					require.NoError(t, err)
					require.True(t, token.Valid)
					return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"access_token":"local-fixture-token","expires_in":3600}`)), Request: r}, nil
				})
				adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: string(credentials), ChannelBaseUrl: "https://aiplatform.googleapis.com"}})
				t.Cleanup(func() { assert.Equal(t, 1, exchanges) })
			}
			result, err := adaptor.ParseTaskResultForTask(task, nil, []byte(tc.completion))
			require.NoError(t, err)
			assert.Equal(t, "SUCCESS", result.Status)
			assert.Empty(t, result.UsageError)
			assert.Equal(t, tc.facts, result.UsageFacts)
			task.PrivateData.PluginRef.SourceHash = strings.Repeat("0", 64)
			_, err = service.LoadTaskPluginForTask(task)
			assert.Error(t, err, "a missing exact source cannot fall back to the active factory")
		})
	}
}

func TestNewAPIVideoArtifactsStayOnGateway(t *testing.T) {
	for _, tc := range []struct{ name, key, data, upstreamID string }{
		{"hailuo v1", "hailuo", `{"status":"Success","file_id":"private-file"}`, "gateway-task"},
		{"hailuo H3", "hailuo", `{"task":{"status":"succeeded","content":{"url":"https://vendor.example/private.mp4"}}}`, "gateway-task"},
		{"vidu", "vidu", `{"state":"success","creations":[{"url":"https://vendor.example/private.mp4"}]}`, "gateway-task"},
		{"google", "google", `{"response":{"generateVideoResponse":{"generatedVideos":[{"video":{"uri":"https://vendor.example/private.mp4"}}]}}}`, "bW9kZWxzL3Zlby9vcGVyYXRpb25zL2dhdGV3YXktdGFzaw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := builtinplugins.Source(tc.key)
			require.NoError(t, err)
			plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
			require.NoError(t, err)
			var data map[string]any
			require.NoError(t, common.Unmarshal([]byte(tc.data), &data))
			artifacts, err := plugin.Engine.Call(t.Context(), "listArtifacts", map[string]any{"status": "SUCCESS", "data": data})
			require.NoError(t, err)
			encoded, err := common.Marshal(artifacts)
			require.NoError(t, err)
			var listed []map[string]any
			require.NoError(t, common.Unmarshal(encoded, &listed))
			require.Len(t, listed, 1)
			artifactKey := listed[0]["key"]
			fetches := 0
			ctx := jsplugin.WithFetcher(t.Context(), func(context.Context, jsplugin.FetchRequest) (jsplugin.FetchResponse, error) {
				fetches++
				return jsplugin.FetchResponse{}, fmt.Errorf("unexpected vendor lookup")
			})
			input := map[string]any{"baseUrl": "https://gateway.example", "apiKey": "gateway-token", "upstream": map[string]any{"kind": "new_api"}, "upstreamTaskId": tc.upstreamID, "artifactKey": artifactKey, "data": data, "clientRequest": map[string]any{"method": "HEAD"}}
			value, err := plugin.Engine.Call(ctx, "buildContentRequest", input)
			require.NoError(t, err)
			assert.Equal(t, map[string]any{"url": "https://gateway.example/v1/tasks/gateway-task/artifacts/video/content", "method": "HEAD", "headers": map[string]any{"Authorization": "Bearer gateway-token"}}, decodePluginValue(t, value))
			if tc.name == "hailuo v1" {
				_, err = plugin.Engine.Call(ctx, "parseTaskResult", input, data)
				require.NoError(t, err)
			}
			assert.Zero(t, fetches, "gateway requests cannot resolve a private vendor file themselves")
			missingID := make(map[string]any, len(input))
			for k, v := range input {
				missingID[k] = v
			}
			missingID["upstreamTaskId"] = ""
			_, err = plugin.Engine.Call(ctx, "buildContentRequest", missingID)
			require.Error(t, err, "missing upstream ID must not use a supplier URL")
		})
	}
}
