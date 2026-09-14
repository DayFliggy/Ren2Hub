package common

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelSystemPromptPreservesStructuredContentAndRetryInput(t *testing.T) {
	for _, test := range []struct {
		name    string
		request any
		want    string
	}{
		{"chat", &dto.GeneralOpenAIRequest{Model: "gpt-5", Messages: []dto.Message{{Role: "developer", Content: []dto.MediaContent{{Type: "text", Text: "existing"}}}}}, `{"model":"gpt-5","messages":[{"role":"developer","content":[{"type":"text","text":"channel"},{"type":"text","text":"existing"}]}]}`},
		{"claude", &dto.ClaudeRequest{Model: "claude-test", System: []any{map[string]any{"type": "text", "text": "existing", "cache_control": map[string]any{"type": "ephemeral"}, "extension": false}}}, `{"model":"claude-test","system":[{"type":"text","text":"channel"},{"type":"text","text":"existing","cache_control":{"type":"ephemeral"},"extension":false}]}`},
		{"responses", &dto.OpenAIResponsesRequest{Model: "gpt-test", Instructions: json.RawMessage(`"existing"`)}, `{"model":"gpt-test","instructions":"channel\nexisting"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, err := common.Marshal(test.request)
			require.NoError(t, err)
			for range 2 {
				var request any
				switch test.request.(type) {
				case *dto.GeneralOpenAIRequest:
					request = &dto.GeneralOpenAIRequest{}
				case *dto.ClaudeRequest:
					request = &dto.ClaudeRequest{}
				case *dto.OpenAIResponsesRequest:
					request = &dto.OpenAIResponsesRequest{}
				}
				require.NoError(t, common.Unmarshal(before, request))
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelSetting: dto.ChannelSettings{SystemPrompt: "channel", SystemPromptOverride: true}}}
				require.NoError(t, ApplyChannelSystemPrompt(c, info, request))
				data, err := common.Marshal(request)
				require.NoError(t, err)
				assert.JSONEq(t, test.want, string(data))
				assert.True(t, common.GetContextKeyBool(c, constant.ContextKeySystemPromptOverride))
			}
			after, err := common.Marshal(test.request)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after))
		})
	}
}

func TestChannelSystemPromptRejectsInvalidInstructions(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Instructions: json.RawMessage(`{"unexpected":"value"}`)}
	info := &RelayInfo{ChannelMeta: &ChannelMeta{ChannelSetting: dto.ChannelSettings{SystemPrompt: "channel", SystemPromptOverride: true}}}
	require.ErrorContains(t, ApplyChannelSystemPrompt(nil, info, request), "instructions must be a string")
	assert.JSONEq(t, `{"unexpected":"value"}`, string(request.Instructions))
}
