package claude_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	awsadapter "github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeMessageOutputConfigBoundaries(t *testing.T) {
	const beta = "mid-conversation-output-config-2026-07-01"
	const body = `{"model":"claude-opus-5-5","max_tokens":64,"system":"original","output_config":{"effort":"medium"},"messages":[{"role":"user","content":"summary"},{"role":"system","content":[],"output_config":{"effort":"high","extension":{"enabled":false,"budget":0}}},{"role":"user","content":"continue"}]}`
	for _, provider := range []string{"claude", "aws", "vertex"} {
		for _, passthrough := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/passthrough=%t/stream=%t", provider, passthrough, stream), func(t *testing.T) {
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Request.Header.Set("anthropic-beta", beta)
					defer common.CleanupBodyStorage(c)
					request, err := helper.GetAndValidateClaudeRequest(c)
					require.NoError(t, err)
					info := &relaycommon.RelayInfo{IsStream: stream, OriginModelName: request.Model, RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{
						UpstreamModelName: request.Model, ChannelBaseUrl: "https://provider.invalid", ApiKey: "synthetic-test-key",
						ChannelSetting:       dto.ChannelSettings{SystemPrompt: "channel", SystemPromptOverride: true, PassThroughBodyEnabled: passthrough},
						ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeAPIKey},
						ParamOverride:        map[string]any{"temperature": 0.25},
					}}
					require.NoError(t, relaycommon.ApplyChannelSystemPrompt(c, info, request))
					var adapter channel.Adaptor
					switch provider {
					case "claude":
						adapter = &claude.Adaptor{}
					case "aws":
						adapter = &awsadapter.Adaptor{}
						info.ApiKey = "synthetic-test-key|us-east-1"
					case "vertex":
						adapter = &vertex.Adaptor{}
					}
					adapter.Init(info)
					var data []byte
					if passthrough {
						reader, closer, err := relaycommon.NativeRequestBody(c, info, request)
						require.NoError(t, err)
						defer closer.Close()
						data, err = io.ReadAll(reader)
						require.NoError(t, err)
					} else {
						converted, err := adapter.ConvertClaudeRequest(c, info, request)
						require.NoError(t, err)
						data, err = common.Marshal(converted)
						require.NoError(t, err)
						data, err = relaycommon.RemoveDisabledFields(data, info.ChannelOtherSettings, false)
						require.NoError(t, err)
						data, err = relaycommon.ApplyParamOverrideWithRelayInfo(data, info)
						require.NoError(t, err)
					}
					if aws, ok := adapter.(*awsadapter.Adaptor); ok {
						_, err = aws.DoRequest(c, info, bytes.NewReader(data))
						require.NoError(t, err)
						if stream {
							data = aws.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput).Body
						} else {
							data = aws.AwsReq.(*bedrockruntime.InvokeModelInput).Body
						}
					} else {
						storage, closer, err := relaycommon.NewOutboundJSONBody(data)
						require.NoError(t, err)
						defer closer.Close()
						outgoing, err := channel.BuildApiRequest(adapter, c, info, storage)
						require.NoError(t, err)
						defer outgoing.Body.Close()
						data, err = io.ReadAll(outgoing.Body)
						require.NoError(t, err)
						require.NotNil(t, outgoing.GetBody)
						replay, err := outgoing.GetBody()
						require.NoError(t, err)
						replayData, err := io.ReadAll(replay)
						require.NoError(t, err)
						require.NoError(t, replay.Close())
						assert.Equal(t, data, replayData)
						assert.Equal(t, beta, outgoing.Header.Get("anthropic-beta"))
					}
					var got struct {
						Messages      []map[string]any `json:"messages"`
						OutputConfig  map[string]any   `json:"output_config"`
						System        string           `json:"system"`
						Temperature   float64          `json:"temperature"`
						AnthropicBeta []string         `json:"anthropic_beta"`
					}
					require.NoError(t, common.Unmarshal(data, &got))
					require.Len(t, got.Messages, 3)
					assert.Equal(t, "system", got.Messages[1]["role"])
					assert.Equal(t, []any{}, got.Messages[1]["content"])
					assert.Equal(t, map[string]any{"effort": "high", "extension": map[string]any{"enabled": false, "budget": float64(0)}}, got.Messages[1]["output_config"])
					assert.NotContains(t, got.Messages[0], "output_config")
					assert.NotContains(t, got.Messages[2], "output_config")
					assert.Equal(t, map[string]any{"effort": "medium"}, got.OutputConfig)
					assert.Equal(t, "channel\noriginal", got.System)
					assert.Equal(t, 0.25, got.Temperature)
					if provider == "aws" {
						assert.Equal(t, []string{beta}, got.AnthropicBeta)
					}
				})
			}
		}
	}
}
