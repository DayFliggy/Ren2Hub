package claude_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/aws"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertClaudeRequestForwardsThinkingBlockBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name         string
		model        string
		thinking     string
		wantThinking string
	}{
		{
			name:         "native request keeps every thinking field",
			model:        "claude-opus-5-5",
			thinking:     `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_field":{"a":1}}`,
			wantThinking: `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_field":{"a":1}}`,
		},
		{
			name:         "effort suffix keeps adaptive binding and unknown controls",
			model:        "claude-opus-5-5-high",
			thinking:     `{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_enabled":false,"future_limit":0,"future_policy":null}`,
			wantThinking: `{"type":"adaptive","display":"summarized","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_enabled":false,"future_limit":0,"future_policy":null}`,
		},
		{
			name:         "suffix changes mode and keeps unknown controls without block_binding",
			model:        "claude-sonnet-5-5-none",
			thinking:     `{"type":"adaptive","block_binding":{"prefix_mismatch_behavior":"drop_block"},"future_enabled":false,"future_limit":0,"future_policy":null}`,
			wantThinking: `{"type":"between_tools","future_enabled":false,"future_limit":0,"future_policy":null}`,
		},
		{
			name:         "request without block_binding gets none",
			model:        "claude-opus-5-5-high",
			thinking:     `{"type":"adaptive"}`,
			wantThinking: `{"type":"adaptive","display":"summarized"}`,
		},
	}
	for _, provider := range []struct {
		name    string
		adaptor channel.Adaptor
	}{
		{"claude", &claude.Adaptor{}},
		{"aws", &aws.Adaptor{}},
		{"vertex", &vertex.Adaptor{}},
	} {
		for _, tt := range tests {
			t.Run(provider.name+"/"+tt.name, func(t *testing.T) {
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				body := `{"model":"` + tt.model + `","max_tokens":1024,"thinking":` + tt.thinking +
					`,"messages":[{"role":"user","content":"hi"}]}`
				var original dto.ClaudeRequest
				require.NoError(t, common.UnmarshalJsonStr(body, &original))
				info := &relaycommon.RelayInfo{
					OriginModelName: tt.model,
					Request:         &original,
					ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: tt.model},
				}
				// The handler applies reasoning before dispatching to any provider.
				outbound, err := common.DeepCopy(&original)
				require.NoError(t, err)
				require.NoError(t, helper.ModelMappedHelper(c, info, outbound))
				require.NoError(t, helper.ApplyReasoningModelSuffix(nil, info, outbound))
				provider.adaptor.Init(info)
				require.NoError(t, relayconvert.ApplyClaudeThinkingModel(outbound, info))

				out, err := provider.adaptor.ConvertClaudeRequest(c, info, outbound)
				require.NoError(t, err)
				encoded, err := common.Marshal(out)
				require.NoError(t, err)
				var upstream struct {
					Thinking json.RawMessage `json:"thinking"`
				}
				require.NoError(t, common.Unmarshal(encoded, &upstream))
				assert.JSONEq(t, tt.wantThinking, string(upstream.Thinking))
			})
		}
	}
}

func TestSetupRequestHeaderForwardsEveryAnthropicBetaValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Add("anthropic-beta", "fast-mode-2026-02-01")
	c.Request.Header.Add("anthropic-beta", "thinking-binding-controls-2026-08-01")
	info := &relaycommon.RelayInfo{
		OriginModelName: "claude-opus-5-5",
		ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: "sk-test"},
	}

	header := http.Header{}
	require.NoError(t, (&claude.Adaptor{}).SetupRequestHeader(c, &header, info))
	assert.Equal(t, []string{"fast-mode-2026-02-01,thinking-binding-controls-2026-08-01"}, header.Values("anthropic-beta"))
}

func TestClaudeResponsePassesInputTransformationsThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-opus-5-5"},
	}
	transformations := `"input_transformations":[{"type":"thinking_dropped","path":"messages.1.content.0","reason":"prefix_binding_mismatch"}]`

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	body := `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[{"type":"text","text":"hi"}],` +
		`"stop_reason":"end_turn",` + transformations + `,"usage":{"input_tokens":5,"output_tokens":1}}`
	require.Nil(t, claude.HandleClaudeResponseData(c, info, &claude.ClaudeResponseInfo{Usage: &dto.Usage{}}, nil, []byte(body)))
	assert.Equal(t, body, w.Body.String())

	w = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	event := `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","content":[],` +
		transformations + `,"usage":{"input_tokens":5,"output_tokens":1}}}`
	require.Nil(t, claude.HandleStreamResponseData(c, info, &claude.ClaudeResponseInfo{Usage: &dto.Usage{}}, event))
	assert.Contains(t, w.Body.String(), "data: "+event+"\n")
}
