package relayconvert

import (
	"context"
	"errors"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestToolMediaUpstreamHoistRetainsResultAndImage(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","max_output_tokens":4096,"input":[{"type":"function_call","call_id":"call_1","name":"screen","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"before"},{"type":"input_image","image_url":"https://example.test/p.png"},{"type":"input_text","text":"after"}]}]}`), &req))
	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatClaude} {
		t.Run(string(target), func(t *testing.T) {
			result, err := ConvertRequest(context.Background(), nil, target, &req)
			require.NoError(t, err)
			raw, err := kitutil.Marshal(result.Value)
			require.NoError(t, err)
			assert.Contains(t, string(raw), "before")
			assert.Contains(t, string(raw), "after")
			assert.Contains(t, string(raw), "call_1")
			if target == types.RelayFormatOpenAI {
				chat := result.Value.(*dto.GeneralOpenAIRequest)
				var tool, user bool
				for _, m := range chat.Messages {
					if m.Role == "tool" {
						tool = true
						assert.NotContains(t, m.StringContent(), "p.png")
					}
					if m.Role == "user" {
						v, _ := kitutil.Marshal(m.Content)
						user = user || strings.Contains(string(v), "p.png")
					}
				}
				assert.True(t, tool)
				assert.True(t, user)
			}
		})
	}
	requirements, err := AnalyzeRequest(&req)
	require.NoError(t, err)
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		plan, err := PlanConversion(requirements, types.RelayFormatGemini, policy)
		require.NoError(t, err, policy)
		assert.Empty(t, plan.Diagnostics, policy)
	}
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatGemini, &req)
	require.NoError(t, err)
	gemini := result.Value.(*dto.GeminiChatRequest)
	require.Len(t, gemini.Contents, 2)
	turn := gemini.Contents[1]
	require.Len(t, turn.Parts, 1)
	response := turn.Parts[0].FunctionResponse
	require.NotNil(t, response)
	assert.Equal(t, "screen", response.Name)
	assert.JSONEq(t, `"call_1"`, string(response.ID))
	assert.Equal(t, map[string]any{"content": "before\nafter"}, response.Response, "media must not be stringified into the tool result")
	assert.JSONEq(t, `[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]`, string(response.Parts))
}

// Parallel tool results keep their order and call IDs. Gemini 3 and unknown
// models carry the media inside functionResponse.parts; Gemini 1.x and 2.x
// receive it as inlineData after every functionResponse of the same turn.
func TestResponsesToolMediaReachesGeminiAsInlineData(t *testing.T) {
	SetMediaResolver(MediaResolver{GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
		header, data, isDataURL := strings.Cut(source.GetRawData(), ",")
		if !isDataURL {
			return "", "", errors.New("download blocked")
		}
		return data, strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), nil
	}})
	t.Cleanup(func() { SetMediaResolver(goldenMediaResolver()) })
	var req dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"client-alias","input":[
		{"type":"function_call","call_id":"call_1","name":"screenshot","arguments":"{}"},
		{"type":"function_call","call_id":"call_2","name":"read_file","arguments":"{}"},
		{"type":"custom_tool_call","call_id":"call_3","name":"report","input":"q3"},
		{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"captured"},{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgo="}]},
		{"type":"function_call_output","call_id":"call_2","output":"file contents"},
		{"type":"custom_tool_call_output","call_id":"call_3","output":[{"type":"input_file","filename":"q3.pdf","file_data":"data:application/pdf;base64,JVBERi0xLjQ="},{"type":"input_image","image_url":"data:image/webp;base64,UklGRg=="}]},
		{"role":"user","content":"what now?"}]}`), &req))

	for _, test := range []struct {
		model, wantTurn string
	}{
		{"gemini-3-pro-preview", `[
			{"functionResponse":{"name":"screenshot","response":{"content":"captured"},"parts":[{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}}],"id":"call_1"}},
			{"functionResponse":{"name":"read_file","response":{"content":"file contents"},"id":"call_2"}},
			{"functionResponse":{"name":"report","response":{},"parts":[{"inlineData":{"mimeType":"application/pdf","data":"JVBERi0xLjQ="}},{"inlineData":{"mimeType":"image/webp","data":"UklGRg=="}}],"id":"call_3"}}]`},
		{"models/gemini-2.5-flash", `[
			{"functionResponse":{"name":"screenshot","response":{"content":"captured"},"id":"call_1"}},
			{"functionResponse":{"name":"read_file","response":{"content":"file contents"},"id":"call_2"}},
			{"functionResponse":{"name":"report","response":{},"id":"call_3"}},
			{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}},
			{"inlineData":{"mimeType":"application/pdf","data":"JVBERi0xLjQ="}},
			{"inlineData":{"mimeType":"image/webp","data":"UklGRg=="}}]`},
	} {
		t.Run(test.model, func(t *testing.T) {
			meta := &convmeta.Values{UpstreamModelName: test.model, ChannelMetaAttached: true}
			result, err := ConvertRequest(context.Background(), meta, types.RelayFormatGemini, &req)
			require.NoError(t, err)
			gemini := result.Value.(*dto.GeminiChatRequest)
			require.Len(t, gemini.Contents, 3)
			turn, err := kitutil.Marshal(gemini.Contents[1].Parts)
			require.NoError(t, err)
			assert.JSONEq(t, test.wantTurn, string(turn))
			assert.Equal(t, []dto.GeminiPart{{Text: "what now?"}}, gemini.Contents[2].Parts)
			// Base64 media never reaches a text field the upstream would tokenize.
			for _, part := range gemini.Contents[1].Parts {
				if part.FunctionResponse != nil {
					response, err := kitutil.Marshal(part.FunctionResponse.Response)
					require.NoError(t, err)
					assert.NotContains(t, string(response), "iVBORw0KGgo=")
					assert.NotContains(t, string(response), "JVBERi0xLjQ=")
				}
			}
		})
	}
}

// Tool media Gemini cannot carry follows the NewAPI path: the converter omits
// it with an error diagnostic, so the conversion policy decides. safe and
// strict reject the request before send; allow sends it without the media and
// records the loss. Media is never written into a text field.
func TestResponsesToolMediaGeminiOmissionFollowsConversionPolicy(t *testing.T) {
	SetMediaResolver(MediaResolver{GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
		header, data, isDataURL := strings.Cut(source.GetRawData(), ",")
		if !isDataURL {
			return "", "", errors.New("download blocked")
		}
		return data, strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), nil
	}})
	t.Cleanup(func() { SetMediaResolver(goldenMediaResolver()) })
	for _, test := range []struct {
		name, output, itemType string
		response               map[string]any
	}{
		{name: "audio", output: `[{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}}]`, response: map[string]any{"content": "[audio]"}},
		{name: "video_file", output: `[{"type":"input_file","file_data":"data:video/mp4;base64,AAAA"}]`, response: map[string]any{"content": "[file]"}},
		{name: "unsupported_mime", output: `[{"type":"input_text","text":"zip"},{"type":"input_file","file_data":"data:application/zip;base64,UEsDBA=="}]`, response: map[string]any{"content": "zip"}},
		// Valid Gemini Blob types outside the documented function-response list.
		{name: "csv_tool_document", output: `[{"type":"input_file","file_data":"data:text/csv;base64,YSxi"}]`, response: map[string]any{"content": "[file]"}},
		{name: "gif_tool_image", output: `[{"type":"input_file","file_data":"data:image/gif;base64,R0lGOA=="}]`, response: map[string]any{"content": "[file]"}},
		// The planner cannot see the resolved MIME type of a URL without a
		// download; the converter omits media that fails to load.
		{name: "unloadable_url", output: `[{"type":"input_text","text":"see"},{"type":"input_image","image_url":"https://example.test/screen.png"}]`, response: map[string]any{"content": "see"}},
		// A single part object is not the documented list shape; NewAPI holds
		// it in the response map as text, which would send base64 as text.
		{name: "single_object", output: `{"type":"input_image","image_url":"data:image/png;base64,YWJj"}`, response: map[string]any{"content": "[tool output omitted]"}},
		// A media part without a source is omitted with a diagnostic instead
		// of being skipped silently.
		{name: "missing_source", output: `[{"type":"input_text","text":"shot"},{"type":"input_image"}]`, response: map[string]any{"content": "shot"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			callType, outputType := "function_call", "function_call_output"
			if test.itemType == "custom_tool_call_output" {
				callType, outputType = "custom_tool_call", "custom_tool_call_output"
			}
			var req dto.OpenAIResponsesRequest
			require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-3-pro-preview","input":[{"type":"`+callType+`","call_id":"call_1","name":"read","arguments":"{}","input":"{}"},{"type":"`+outputType+`","call_id":"call_1","output":`+test.output+`}]}`), &req))
			requirements, err := AnalyzeRequest(&req)
			require.NoError(t, err)
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}, UpstreamModelName: "gemini-3-pro-preview", ChannelMetaAttached: true}
				// Planning leaves the decision to the converter, which knows the
				// Gemini version; it reports the loss once, as unsupported_media_type.
				plan, planErr := PlanConversion(requirements, types.RelayFormatGemini, policy)
				require.NoError(t, planErr, policy)
				assert.NotContains(t, diagnosticCodes(plan.Diagnostics), "media_conversion_unavailable", policy)
				if policy != types.ConversionLossPolicyAllow {
					_, err := ExecuteConversionPlan(context.Background(), meta, plan, &req)
					var loss *types.ConversionLossError
					require.ErrorAs(t, err, &loss, "%s must reject omitted media before send", policy)
					assert.Equal(t, "unsupported_media_type", loss.Diagnostics[0].Code)
					continue
				}
				result, err := ExecuteConversionPlan(context.Background(), meta, plan, &req)
				require.NoError(t, err)
				assert.Contains(t, diagnosticCodes(result.Diagnostics), "unsupported_media_type")
				gemini := result.Value.(*dto.GeminiChatRequest)
				require.Len(t, gemini.Contents, 2)
				require.Len(t, gemini.Contents[1].Parts, 1, "omitted media sends no inline part")
				response := gemini.Contents[1].Parts[0].FunctionResponse
				require.NotNil(t, response)
				assert.Equal(t, test.response, response.Response)
				assert.Empty(t, response.Parts)
				encoded, err := kitutil.Marshal(gemini)
				require.NoError(t, err)
				for _, data := range []string{"YWJj", "UklGRg==", "UEsDBA==", "YSxi", "R0lGOA==", "AAAA"} {
					assert.NotContains(t, string(encoded), data, "media must not reach a text field")
				}
			}
		})
	}
}

// Provider-owned media keeps its native-only rule under every policy.
func TestResponsesToolMediaGeminiRejectsOwnedMediaUnderEveryPolicy(t *testing.T) {
	var req dto.OpenAIResponsesRequest
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-3-pro-preview","input":[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_file","file_url":"gs://bucket/one.pdf"}]}]}`), &req))
	requirements, err := AnalyzeRequest(&req)
	require.NoError(t, err)
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		_, err := PlanConversion(requirements, types.RelayFormatGemini, policy)
		var loss *types.ConversionLossError
		require.ErrorAs(t, err, &loss, policy)
		assert.Equal(t, "native_state_required", loss.Diagnostics[0].Code)
	}
}

func diagnosticCodes(diagnostics []types.ConversionDiagnostic) []string {
	codes := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	return codes
}

// Gemini 1.x and 2.x take tool media as ordinary parts after the turn's
// function responses, so audio and video that Gemini accepts reach them
// under every policy without a loss diagnostic. Gemini 3 carries tool media
// inside functionResponse.parts, which has no audio or video, so the same
// request is rejected before send under safe.
func TestResponsesToolAudioVideoFollowsTheGeminiVersion(t *testing.T) {
	SetMediaResolver(MediaResolver{GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
		header, data, isDataURL := strings.Cut(source.GetRawData(), ",")
		if !isDataURL {
			return source.GetRawData(), "audio/wav", nil
		}
		return data, strings.TrimSuffix(strings.TrimPrefix(header, "data:"), ";base64"), nil
	}})
	t.Cleanup(func() { SetMediaResolver(goldenMediaResolver()) })
	const output = `[{"type":"input_text","text":"clip"},{"type":"input_audio","input_audio":{"data":"UklGRg==","format":"wav"}},{"type":"input_file","file_data":"data:video/mp4;base64,AAAA"}]`
	decode := func(model string) *dto.OpenAIResponsesRequest {
		var req dto.OpenAIResponsesRequest
		require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"`+model+`","input":[{"type":"function_call","call_id":"call_1","name":"record","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":`+output+`}]}`), &req))
		return &req
	}
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		t.Run("gemini-2.5-flash/"+string(policy), func(t *testing.T) {
			meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}, UpstreamModelName: "gemini-2.5-flash", ChannelMetaAttached: true}
			result, err := ConvertRequest(context.Background(), meta, types.RelayFormatGemini, decode("gemini-2.5-flash"))
			require.NoError(t, err)
			assert.NotContains(t, diagnosticCodes(result.Diagnostics), "media_conversion_unavailable")
			assert.NotContains(t, diagnosticCodes(result.Diagnostics), "unsupported_media_type")
			gemini := result.Value.(*dto.GeminiChatRequest)
			turn, err := kitutil.Marshal(gemini.Contents[1].Parts)
			require.NoError(t, err)
			assert.JSONEq(t, `[
				{"functionResponse":{"name":"record","response":{"content":"clip"},"id":"call_1"}},
				{"inlineData":{"mimeType":"audio/wav","data":"UklGRg=="}},
				{"inlineData":{"mimeType":"video/mp4","data":"AAAA"}}]`, string(turn))
		})
	}
	meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicySafe}, UpstreamModelName: "gemini-3-pro-preview", ChannelMetaAttached: true}
	_, err := ConvertRequest(context.Background(), meta, types.RelayFormatGemini, decode("gemini-3-pro-preview"))
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	assert.Equal(t, []string{"unsupported_media_type", "unsupported_media_type"}, diagnosticCodes(loss.Diagnostics))
}

// A tool output that mixes text and media with a part Gemini has no
// equivalent for loses only that part: allow sends the text and the media it
// can carry with an unsupported_media_type diagnostic, and safe and strict
// reject it before send. A list without media keeps NewAPI's response map.
func TestResponsesToolOutputDropsOnlyUnrepresentablePartsForGemini(t *testing.T) {
	const output = `[{"type":"input_text","text":"first"},{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgo="},{"type":"refusal","refusal":"no"},{"type":"input_text","text":"second"}]`
	decode := func(output string) *dto.OpenAIResponsesRequest {
		var req dto.OpenAIResponsesRequest
		require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-3-pro-preview","input":[{"type":"function_call","call_id":"call_1","name":"read","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":`+output+`}]}`), &req))
		return &req
	}
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
		meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: policy}, UpstreamModelName: "gemini-3-pro-preview", ChannelMetaAttached: true}
		_, err := ConvertRequest(context.Background(), meta, types.RelayFormatGemini, decode(output))
		var loss *types.ConversionLossError
		require.ErrorAs(t, err, &loss, policy)
		assert.Equal(t, []string{"unsupported_media_type"}, diagnosticCodes(loss.Diagnostics), policy)
	}

	meta := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicyAllow}, UpstreamModelName: "gemini-3-pro-preview", ChannelMetaAttached: true}
	result, err := ConvertRequest(context.Background(), meta, types.RelayFormatGemini, decode(output))
	require.NoError(t, err)
	assert.Contains(t, diagnosticCodes(result.Diagnostics), "unsupported_media_type")
	response := result.Value.(*dto.GeminiChatRequest).Contents[1].Parts[0].FunctionResponse
	require.NotNil(t, response)
	assert.Equal(t, map[string]any{"content": "first\nsecond"}, response.Response)
	assert.JSONEq(t, `[{"inlineData":{"mimeType":"image/png","data":"aGVsbG8="}}]`, string(response.Parts))

	result, err = ConvertRequest(context.Background(), meta, types.RelayFormatGemini, decode(`[{"type":"input_text","text":"plain"},{"type":"refusal","refusal":"no"}]`))
	require.NoError(t, err)
	assert.NotContains(t, diagnosticCodes(result.Diagnostics), "unsupported_media_type")
	assert.Equal(t, map[string]any{"result": []any{map[string]any{"type": "input_text", "text": "plain"}, map[string]any{"type": "refusal", "refusal": "no"}}}, result.Value.(*dto.GeminiChatRequest).Contents[1].Parts[0].FunctionResponse.Response)
}
