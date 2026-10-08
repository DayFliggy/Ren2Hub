package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const legacyFunctionPairBody = `{"model":"test","max_tokens":64,
	"functions":[{"name":"get_weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}],
	"messages":[
		{"role":"user","content":"Weather in Paris?"},
		{"role":"assistant","content":null,"function_call":{"name":"get_weather","arguments":"{\"city\":\"Paris\"}"}},
		{"role":"function","name":"get_weather","content":"sunny"},
		{"role":"user","content":"Thanks"}]}`

func legacyChatRequest(t *testing.T, body string) *dto.GeneralOpenAIRequest {
	t.Helper()
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(body), request))
	return request
}

func TestLegacyFunctionCallPairBecomesToolExchange(t *testing.T) {
	for _, tc := range []struct {
		target types.RelayFormat
		check  func(t *testing.T, encoded []byte)
	}{
		{types.RelayFormatOpenAIResponses, func(t *testing.T, encoded []byte) {
			call := gjson.GetBytes(encoded, `input.#(type=="function_call")`)
			assert.Equal(t, "call_legacy_1", call.Get("call_id").String())
			assert.Equal(t, "get_weather", call.Get("name").String())
			assert.Equal(t, `{"city":"Paris"}`, call.Get("arguments").String())
			output := gjson.GetBytes(encoded, `input.#(type=="function_call_output")`)
			assert.Equal(t, "call_legacy_1", output.Get("call_id").String())
			assert.Equal(t, "sunny", output.Get("output").String())
			assert.NotContains(t, string(encoded), "tool_output_missing_call_id")
		}},
		{types.RelayFormatClaude, func(t *testing.T, encoded []byte) {
			use := gjson.GetBytes(encoded, `messages.1.content.#(type=="tool_use")`)
			assert.Equal(t, "call_legacy_1", use.Get("id").String())
			assert.JSONEq(t, `{"city":"Paris"}`, use.Get("input").Raw)
			assert.Equal(t, "tool_result", gjson.GetBytes(encoded, "messages.2.content.0.type").String())
			assert.Equal(t, "call_legacy_1", gjson.GetBytes(encoded, "messages.2.content.0.tool_use_id").String())
		}},
		{types.RelayFormatGemini, func(t *testing.T, encoded []byte) {
			// Gemini matches results by name; a synthetic id would only be
			// projected away again for name-only adapters.
			assert.Equal(t, "get_weather", gjson.GetBytes(encoded, "contents.1.parts.0.functionCall.name").String())
			assert.False(t, gjson.GetBytes(encoded, "contents.1.parts.0.functionCall.id").Exists())
			assert.Equal(t, "get_weather", gjson.GetBytes(encoded, "contents.2.parts.0.functionResponse.name").String())
			assert.False(t, gjson.GetBytes(encoded, "contents.2.parts.0.functionResponse.id").Exists())
		}},
	} {
		t.Run(string(tc.target), func(t *testing.T) {
			request := legacyChatRequest(t, legacyFunctionPairBody)
			info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyStrict}}
			result, err := ConvertRequest(context.Background(), info, tc.target, request)
			require.NoError(t, err, "a paired legacy exchange is not a declared loss")
			encoded, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			tc.check(t, encoded)
			assert.NotEmpty(t, request.Messages[1].FunctionCall, "conversion must not modify the client history")
			assert.Equal(t, "function", request.Messages[2].Role)
		})
	}

	t.Run("synthetic id avoids client ids", func(t *testing.T) {
		request := legacyChatRequest(t, `{"model":"test","messages":[
			{"role":"assistant","tool_calls":[{"id":"call_legacy_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"call_legacy_1","content":"modern"},
			{"role":"assistant","function_call":{"name":"lookup","arguments":"{}"}},
			{"role":"function","name":"lookup","content":"legacy"}]}`)
		result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, request)
		require.NoError(t, err)
		encoded, err := kitutil.Marshal(result.Value)
		require.NoError(t, err)
		assert.Equal(t, []string{"call_legacy_1", "call_legacy_2"}, stringsOf(gjson.GetBytes(encoded, `input.#(type=="function_call")#.call_id`)))
		assert.Equal(t, []string{"call_legacy_1", "call_legacy_2"}, stringsOf(gjson.GetBytes(encoded, `input.#(type=="function_call_output")#.call_id`)))
	})

	t.Run("vertex name-only analysis agrees with execution", func(t *testing.T) {
		request := legacyChatRequest(t, legacyFunctionPairBody)
		requirements, err := AnalyzeRequest(request)
		require.NoError(t, err)
		diagnostics, err := requirements.ToolIdentity.CheckNameOnly(types.RelayFormatOpenAI, types.ConversionLossPolicyStrict)
		require.NoError(t, err)
		assert.Empty(t, diagnostics)
		converted, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, request)
		require.NoError(t, err)
		projected, err := ProjectGeminiToolHistoryWithoutIDs(converted.Value.(*dto.GeminiChatRequest), types.ConversionLossPolicyStrict)
		require.NoError(t, err)
		assert.Empty(t, projected.Diagnostics)
	})
}

func TestUnpairedLegacyFunctionHistoryIsDiagnosed(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		codes      []string
	}{
		{"call without result", `{"model":"test","max_tokens":64,"messages":[{"role":"user","content":"hi"},{"role":"assistant","function_call":{"name":"lookup","arguments":"{}"}},{"role":"user","content":"next"}]}`, []string{"legacy_function_call_unpaired"}},
		{"result name mismatch", `{"model":"test","max_tokens":64,"messages":[{"role":"user","content":"hi"},{"role":"assistant","function_call":{"name":"lookup","arguments":"{}"}},{"role":"function","name":"other","content":"x"}]}`, []string{"legacy_function_call_unpaired", "legacy_function_result_unpaired"}},
		{"result without call", `{"model":"test","max_tokens":64,"messages":[{"role":"user","content":"hi"},{"role":"function","name":"lookup","content":"x"}]}`, []string{"legacy_function_result_unpaired"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requirements, err := AnalyzeRequest(legacyChatRequest(t, tc.body))
			require.NoError(t, err)
			_, err = PlanConversion(requirements, types.RelayFormatOpenAI, types.ConversionLossPolicyStrict)
			require.NoError(t, err, "native Chat forwarding keeps the legacy history")

			for _, target := range []types.RelayFormat{types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini} {
				for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
					info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}}
					_, err := ConvertRequest(context.Background(), info, target, legacyChatRequest(t, tc.body))
					var lossErr *types.ConversionLossError
					require.ErrorAs(t, err, &lossErr, "%s %s", target, policy)
				}

				info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}}
				result, err := ConvertRequest(context.Background(), info, target, legacyChatRequest(t, tc.body))
				require.NoError(t, err, target)
				var codes []string
				for _, diagnostic := range result.Diagnostics {
					if diagnostic.Code == "legacy_function_call_unpaired" || diagnostic.Code == "legacy_function_result_unpaired" {
						codes = append(codes, diagnostic.Code)
					}
				}
				assert.Equal(t, tc.codes, codes, target)
				encoded, err := kitutil.Marshal(result.Value)
				require.NoError(t, err)
				// An unanswered call is omitted rather than sent as a call the
				// target would reject for lacking its result.
				for _, marker := range []string{`"function_call"`, `"tool_use"`, `"functionCall"`, `"tool_calls"`} {
					assert.NotContains(t, string(encoded), marker, target)
				}
			}
		})
	}

	_, err := AnalyzeRequest(legacyChatRequest(t, `{"model":"test","messages":[{"role":"assistant","function_call":{"arguments":"{}"}}]}`))
	require.ErrorContains(t, err, "messages[0].function_call requires name")
}

func stringsOf(result gjson.Result) []string {
	values := make([]string, 0)
	for _, item := range result.Array() {
		values = append(values, item.String())
	}
	return values
}
