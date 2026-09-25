package ollama

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOllamaOutputLimitPreservesExplicitZero(t *testing.T) {
	for _, body := range []string{`{}`, `{"max_tokens":0}`, `{"max_tokens":8,"max_completion_tokens":0}`} {
		var request dto.GeneralOpenAIRequest
		require.NoError(t, common.Unmarshal([]byte(body), &request))
		for _, convert := range []func() (any, error){
			func() (any, error) { return openAIChatToOllamaChat(nil, &request) },
			func() (any, error) { return openAIToGenerate(nil, &request) },
		} {
			result, err := convert()
			require.NoError(t, err)
			encoded, err := common.Marshal(result)
			require.NoError(t, err)
			limit := gjson.GetBytes(encoded, "options.num_predict")
			assert.Equal(t, body != `{}`, limit.Exists())
			assert.Zero(t, limit.Int())
		}
	}
}

func TestOllamaToolHistoryPreservesIdentityAndArguments(t *testing.T) {
	for _, test := range []struct {
		name, body string
		format     types.RelayFormat
	}{
		{"chat", `{"model":"llama3.1","messages":[{"role":"user","content":"first"},{"role":"assistant","tool_calls":[{"id":"first","type":"function","function":{"name":"lookup","arguments":"{\"integer\":9007199254740993,\"zero\":0,\"disabled\":false}"}}]},{"role":"tool","tool_call_id":"first","content":"first-result"},{"role":"assistant","tool_calls":[{"id":"second","type":"function","function":{"name":"lookup","arguments":"{\"n\":2}"}},{"id":"parallel","type":"function","function":{"name":"lookup","arguments":"{\"n\":3}"}}]},{"role":"tool","tool_call_id":"parallel","content":"parallel-result"},{"role":"tool","tool_call_id":"second","content":"second-result"}]}`, types.RelayFormatOpenAI},
		{"claude", `{"model":"llama3.1","messages":[{"role":"user","content":"first"},{"role":"assistant","content":[{"type":"tool_use","id":"first","name":"lookup","input":{"integer":"9007199254740993","zero":0,"disabled":false}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"first","content":"first-result"}]},{"role":"assistant","content":[{"type":"tool_use","id":"second","name":"lookup","input":{"n":2}},{"type":"tool_use","id":"parallel","name":"lookup","input":{"n":3}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"parallel","content":"parallel-result"},{"type":"tool_result","tool_use_id":"second","content":"second-result"}]}]}`, types.RelayFormatClaude},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{RelayFormat: test.format, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}
			adaptor := &Adaptor{}
			var converted any
			var err error
			if test.format == types.RelayFormatOpenAI {
				request := &dto.GeneralOpenAIRequest{}
				require.NoError(t, common.Unmarshal([]byte(test.body), request))
				before, err := common.Marshal(request)
				require.NoError(t, err)
				converted, err = adaptor.ConvertOpenAIRequest(c, info, request)
				require.NoError(t, err)
				after, err := common.Marshal(request)
				require.NoError(t, err)
				assert.JSONEq(t, string(before), string(after), "retries retain the original request")
			} else {
				request := &dto.ClaudeRequest{}
				require.NoError(t, common.Unmarshal([]byte(test.body), request))
				converted, err = adaptor.ConvertClaudeRequest(c, info, request)
				require.NoError(t, err)
				assert.Same(t, request, converted)
				return
			}
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			assert.Equal(t, "first", gjson.GetBytes(body, "messages.1.tool_calls.0.id").String())
			assert.Equal(t, "9007199254740993", gjson.GetBytes(body, "messages.1.tool_calls.0.function.arguments.integer").String())
			assert.Equal(t, "0", gjson.GetBytes(body, "messages.1.tool_calls.0.function.arguments.zero").Raw)
			assert.Equal(t, "false", gjson.GetBytes(body, "messages.1.tool_calls.0.function.arguments.disabled").Raw)
			assert.Equal(t, "lookup", gjson.GetBytes(body, "messages.2.tool_name").String())
			assert.Equal(t, "first", gjson.GetBytes(body, "messages.2.tool_call_id").String())
			assert.Equal(t, "parallel", gjson.GetBytes(body, "messages.4.tool_call_id").String())
			assert.Equal(t, "second", gjson.GetBytes(body, "messages.5.tool_call_id").String())
			assert.False(t, info.UpstreamSent)
			assert.False(t, c.Writer.Written())
		})
	}
}

func TestOllamaMissingToolIDsUseUnambiguousHistory(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{"messages":[{"role":"assistant","tool_calls":[{"type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","name":"lookup","content":"first"},{"role":"assistant","tool_calls":[{"id":"call_ollama_0","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"call_ollama_0","content":"second"}]}`), request))
	converted, err := openAIChatToOllamaChat(nil, request)
	require.NoError(t, err)
	assert.Equal(t, "call_ollama_1", converted.Messages[0].ToolCalls[0].ID)
	assert.Equal(t, "call_ollama_1", converted.Messages[1].ToolCallID)
	assert.Equal(t, "call_ollama_0", converted.Messages[3].ToolCallID)
}

func TestOllamaRejectsInvalidHistoryBeforeMediaResolution(t *testing.T) {
	for _, test := range []struct{ name, messages, want string }{
		{"malformed_calls", `[{"role":"assistant","tool_calls":{}}]`, "tool_calls"},
		{"malformed_arguments", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{invalid"}}]}]`, "arguments"},
		{"nonobject_arguments", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"null"}}]}]`, "arguments"},
		{"missing_result", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]`, "missing tool results"},
		{"orphan_result", `[{"role":"tool","tool_call_id":"missing","content":"bad"}]`, "no matching outstanding call"},
		{"ambiguous_result", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}},{"id":"b","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","name":"lookup","content":"bad"}]`, "ambiguous"},
		{"duplicate_result", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","content":"first"},{"role":"tool","tool_call_id":"a","content":"duplicate"}]`, "no matching outstanding call"},
		{"duplicate_id", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}},{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]}]`, "duplicate tool call"},
		{"name_mismatch", `[{"role":"assistant","tool_calls":[{"id":"a","type":"function","function":{"name":"lookup","arguments":"{}"}}]},{"role":"tool","tool_call_id":"a","name":"different","content":"bad"}]`, "no matching outstanding call"},
		{"audio", `[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AA==","format":"wav"}}]}]`, "input_audio"},
		{"unknown_content", `[{"role":"user","content":[{"type":"future_media","data":"data"}]}]`, "future_media"},
		{"invalid_image", `[{"role":"user","content":[{"type":"image_url","image_url":{}}]}]`, "no image source"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := &dto.GeneralOpenAIRequest{}
			require.NoError(t, common.Unmarshal([]byte(`{"messages":`+test.messages+`}`), request))
			// A bad history must fail before trying to fetch this earlier image.
			request.Messages = append([]dto.Message{{Role: "user", Content: []dto.MediaContent{{Type: dto.ContentTypeImageURL, ImageUrl: &dto.MessageImageUrl{Url: "https://must-not-resolve.invalid/image"}}}}}, request.Messages...)
			_, err := openAIChatToOllamaChat(nil, request)
			require.ErrorContains(t, err, test.want)
		})
	}
}

func TestOllamaStructuredOutputAndImages(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}}},"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a4Z0AAAAASUVORK5CYII="}}]}]}`), request))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	converted, err := openAIChatToOllamaChat(c, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.Equal(t, "false", gjson.GetBytes(body, "format.additionalProperties").Raw)
	assert.Equal(t, "describe", converted.Messages[0].Content)
	require.Len(t, converted.Messages[0].Images, 1)
	assert.NotContains(t, converted.Messages[0].Images[0], "data:")
	for _, format := range []string{`{"type":"json_schema"}`, `{"type":"json_schema","json_schema":{"name":"bad"}}`, `{"type":"json_schema","json_schema":{"schema":"bad"}}`, `{"type":"unknown"}`} {
		var responseFormat dto.ResponseFormat
		require.NoError(t, common.Unmarshal([]byte(format), &responseFormat))
		_, err := toOllamaResponseFormat(&responseFormat)
		require.Error(t, err)
	}
}

func TestOllamaToolConstraintsAreEnforcedBeforeSending(t *testing.T) {
	for _, body := range []string{
		`{"tool_choice":"required"}`,
		`{"tool_choice":{"type":"function","function":{"name":"lookup"}}}`,
		`{"parallel_tool_calls":false}`,
		`{"tools":[{"type":"function","function":{"name":"lookup","strict":true,"parameters":{"type":"object"}}}]}`,
		`{"functions":[{"name":"lookup","parameters":{"type":"object"}}]}`,
	} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
			request := &dto.GeneralOpenAIRequest{}
			require.NoError(t, common.Unmarshal([]byte(body), request))
			info := &relaycommon.RelayInfo{ConversionPolicy: policy}
			_, err := (&Adaptor{}).ConvertOpenAIRequest(nil, info, request)
			require.Error(t, err)
			var loss *types.ConversionLossError
			require.ErrorAs(t, err, &loss)
			assert.NotEmpty(t, info.ConversionDiagnostics)
			assert.False(t, info.UpstreamSent)
		}
	}
	for _, choice := range []string{"auto", "none"} {
		request := &dto.GeneralOpenAIRequest{ToolChoice: choice, Tools: []dto.ToolCallRequest{{Type: "function", Function: dto.FunctionRequest{Name: "lookup", Parameters: map[string]any{"type": "object"}}}}}
		converted, err := openAIChatToOllamaChat(nil, request)
		require.NoError(t, err)
		if choice == "none" {
			assert.Nil(t, converted.Tools)
		} else {
			assert.NotNil(t, converted.Tools)
		}
		assert.Len(t, request.Tools, 1, "tool selection must not mutate retry input")
	}
	request := &dto.ClaudeRequest{}
	require.NoError(t, common.Unmarshal([]byte(`{"model":"llama3.1","tool_choice":{"type":"any"},"tools":[{"name":"lookup","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"lookup"}]}`), request))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "llama3.1"}}
	converted, err := (&Adaptor{}).ConvertClaudeRequest(c, info, request)
	require.NoError(t, err)
	assert.Same(t, request, converted)
	assert.Empty(t, info.ConversionDiagnostics)
	assert.False(t, c.Writer.Written())
}
