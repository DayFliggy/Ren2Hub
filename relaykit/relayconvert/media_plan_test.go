package relayconvert

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMediaPlanRejectsUnexecutableConversionInEveryPolicy(t *testing.T) {
	for _, test := range []struct {
		name, body string
		request    any
		target     types.RelayFormat
	}{
		{"claude_document", `{"model":"test","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"claude_document_via_chat", `{"model":"test","messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERg=="}}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatGemini},
		{"claude_missing_image", `{"model":"test","messages":[{"role":"user","content":[{"type":"image"}]}]}`, &dto.ClaudeRequest{}, types.RelayFormatOpenAI},
		{"gemini_audio", `{"contents":[{"parts":[{"inlineData":{"mimeType":"audio/wav","data":"YWJj"}}]}]}`, &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"gemini_pdf", `{"contents":[{"parts":[{"inlineData":{"mimeType":"application/pdf","data":"JVBERg=="}}]}]}`, &dto.GeminiChatRequest{}, types.RelayFormatOpenAIResponses},
		{"gemini_video", `{"contents":[{"parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"https://media.example/video.mp4"}}]}]}`, &dto.GeminiChatRequest{}, types.RelayFormatClaude},
		{"gemini_private_file", `{"contents":[{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"https://generativelanguage.googleapis.com/v1beta/files/private-one"}}]}]}`, &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"gemini_private_bucket", `{"contents":[{"parts":[{"fileData":{"mimeType":"image/png","fileUri":"gs://private-bucket/image.png"}}]}]}`, &dto.GeminiChatRequest{}, types.RelayFormatOpenAI},
		{"chat_audio", `{"model":"test","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatClaude},
		{"responses_audio", `{"model":"test","input":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}]}]}`, &dto.OpenAIResponsesRequest{}, types.RelayFormatClaude},
		{"chat_wrong_image_mime", `{"model":"test","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:audio/wav;base64,YWJj"}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatClaude},
		{"chat_audio_without_format", `{"model":"test","messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"YWJj"}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
		{"chat_file_without_filename", `{"model":"test","messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"data:application/pdf;base64,JVBERg=="}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
		{"chat_file_url", `{"model":"test","messages":[{"role":"user","content":[{"type":"file","file":{"filename":"one.pdf","file_url":"https://files.example/one.pdf"}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
		{"chat_video_object", `{"model":"test","messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://files.example/one.mp4"}}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatGemini},
		{"chat_responses_image_shape", `{"model":"test","messages":[{"role":"user","content":[{"type":"input_image","image_url":"https://images.example/one.png"}]}]}`, &dto.GeneralOpenAIRequest{}, types.RelayFormatClaude},
		{"responses_file_url_to_chat", `{"model":"test","input":[{"role":"user","content":[{"type":"input_file","file_url":"https://files.example/one.pdf"}]}]}`, &dto.OpenAIResponsesRequest{}, types.RelayFormatOpenAI},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
			requirements, err := AnalyzeRequest(test.request)
			require.NoError(t, err)
			require.Len(t, requirements.Media, 1)
			metadata, err := kitutil.Marshal(requirements.Media)
			require.NoError(t, err)
			assert.NotContains(t, string(metadata), "private-one")
			assert.NotContains(t, string(metadata), "YWJj")
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				plan, err := PlanConversion(requirements, test.target, policy)
				require.Error(t, err, string(policy))
				require.NotEmpty(t, plan.Diagnostics)
				_, err = ExecuteConversionPlan(context.Background(), nil, plan, test.request)
				require.Error(t, err, "a manually replayed rejected plan must not drop media")
				_, err = PlanConversion(requirements, requirements.Format, policy)
				require.NoError(t, err, "native media semantics belong to the original protocol")
			}
		})
	}
}

func TestMediaPlanImageURLsArePreserved(t *testing.T) {
	for _, test := range []struct {
		name, body string
		request    any
	}{
		{"claude", `{"model":"test","messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://images.example/picture.png?size=2"}}]}]}`, &dto.ClaudeRequest{}},
		{"gemini", `{"contents":[{"role":"user","parts":[{"fileData":{"mimeType":"image/png","fileUri":"https://images.example/picture.png?size=2"}}]}]}`, &dto.GeminiChatRequest{}},
	} {
		for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
			t.Run(test.name+"/"+string(policy), func(t *testing.T) {
				require.NoError(t, kitutil.Unmarshal([]byte(test.body), test.request))
				requirements, err := AnalyzeRequest(test.request)
				require.NoError(t, err)
				require.Len(t, requirements.Media, 1)
				assert.Equal(t, "image", requirements.Media[0].Kind)
				plan, err := PlanConversion(requirements, types.RelayFormatOpenAI, policy)
				require.NoError(t, err)
				result, err := ExecuteConversionPlan(context.Background(), &convmeta.Values{UpstreamModelName: "test", ChannelMetaAttached: true}, plan, test.request)
				require.NoError(t, err)
				request := result.Value.(*dto.GeneralOpenAIRequest)
				require.Len(t, request.Messages, 1)
				parts := request.Messages[0].ParseContent()
				require.Len(t, parts, 1)
				assert.Equal(t, "https://images.example/picture.png?size=2", parts[0].GetImageMedia().Url)
			})
		}
	}
}

func TestMediaPlanPreservesExistingGeminiInputs(t *testing.T) {
	SetMediaResolver(MediaResolver{GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
		inline, ok := source.(*types.Base64Source)
		require.True(t, ok, "inline input must not be downloaded")
		if strings.HasPrefix(inline.Base64Data, "data:") {
			header, data, found := strings.Cut(strings.TrimPrefix(inline.Base64Data, "data:"), ",")
			require.True(t, found)
			mimeType, _, _ := strings.Cut(header, ";")
			return data, mimeType, nil
		}
		return inline.Base64Data, inline.MimeType, nil
	}})
	t.Cleanup(func() { SetMediaResolver(goldenMediaResolver()) })
	for _, test := range []struct {
		request        any
		mimeType, data string
	}{
		{&dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: []any{map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": "YWJj", "format": "wav"}}}}}}, "audio/wav", "YWJj"},
		{&dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`[{"role":"user","content":[{"type":"input_file","file_data":"data:application/pdf;base64,JVBERg=="}]}]`)}, "application/pdf", "JVBERg=="},
	} {
		requirements, err := AnalyzeRequest(test.request)
		require.NoError(t, err)
		plan, err := PlanConversion(requirements, types.RelayFormatGemini, types.ConversionLossPolicySafe)
		require.NoError(t, err)
		result, err := ExecuteConversionPlan(context.Background(), nil, plan, test.request)
		require.NoError(t, err)
		converted := result.Value.(*dto.GeminiChatRequest)
		require.NotEmpty(t, converted.Contents)
		require.NotNil(t, converted.Contents[0].Parts[0].InlineData)
		assert.Equal(t, test.mimeType, converted.Contents[0].Parts[0].InlineData.MimeType)
		assert.Equal(t, test.data, converted.Contents[0].Parts[0].InlineData.Data)
	}
}

func TestMediaOwnershipURIClassification(t *testing.T) {
	for _, uri := range []string{"gs://bucket/object", "https://generativelanguage.googleapis.com/v1beta/files/one", "https://generativelanguage.googleapis.com/v1/files/one", "https://generativelanguage.googleapis.com/v1alpha/files/one?alt=media"} {
		assert.True(t, RequiresMediaOwnership(uri), uri)
	}
	for _, uri := range []string{"https://images.example/image.png", "data:image/png;base64,YWJj", "https://generativelanguage.googleapis.com.evil.example/v1beta/files/one", "https://images.example/files/image.png", "invalid%url"} {
		assert.False(t, RequiresMediaOwnership(uri), uri)
	}
}

func TestMediaImageDetailLossIsExplicit(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"test","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://images.example/one.png","detail":"high"}}]}]}`), request))
	requirements, err := AnalyzeRequest(request)
	require.NoError(t, err)
	for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
		plan, err := PlanConversion(requirements, target, types.ConversionLossPolicySafe)
		require.NoError(t, err)
		require.NotEmpty(t, plan.Diagnostics)
		assert.True(t, strings.HasSuffix(plan.Diagnostics[0].Path, ".detail"))
		_, err = PlanConversion(requirements, target, types.ConversionLossPolicyStrict)
		require.Error(t, err)
	}
}

func TestMediaChatResponsesImageDetailIsPreserved(t *testing.T) {
	for _, detail := range []string{"", "low", "high", "auto"} {
		for _, from := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				t.Run(fmt.Sprintf("%s/%s/%s", from, detail, policy), func(t *testing.T) {
					image := map[string]any{"url": "https://images.example/detail.png"}
					if detail != "" {
						image["detail"] = detail
					}
					var request any = &dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: []any{map[string]any{"type": "image_url", "image_url": image}}}}}
					target := types.RelayFormat(types.RelayFormatOpenAIResponses)
					if from == types.RelayFormatOpenAIResponses {
						part := map[string]any{"type": "input_image", "image_url": image["url"]}
						if detail != "" {
							part["detail"] = detail
						}
						input, err := kitutil.Marshal([]any{map[string]any{"role": "user", "content": []any{part}}})
						require.NoError(t, err)
						request = &dto.OpenAIResponsesRequest{Model: "test", Input: input}
						target = types.RelayFormatOpenAI
					}
					requirements, err := AnalyzeRequest(request)
					require.NoError(t, err)
					plan, err := PlanConversion(requirements, target, policy)
					require.NoError(t, err)
					require.Empty(t, plan.Diagnostics)
					result, err := ExecuteConversionPlan(context.Background(), nil, plan, request)
					require.NoError(t, err)
					part := mediaTestConvertedPart(t, result.Value)
					if target == types.RelayFormatOpenAI {
						parsed := result.Value.(*dto.GeneralOpenAIRequest).Messages[0].ParseContent()
						require.Len(t, parsed, 1)
						assert.Equal(t, detail, parsed[0].GetImageMedia().Detail)
						if detail != "" {
							part = part["image_url"].(map[string]any)
						}
					}
					if detail == "" {
						assert.NotContains(t, part, "detail")
					} else {
						assert.Equal(t, detail, part["detail"])
					}
				})
			}
		}
	}
}

func TestMediaChatResponsesExistingPayloadsArePreserved(t *testing.T) {
	for _, test := range []struct{ name, chat, responses string }{
		{"audio", `{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}`, `{"type":"input_audio","input_audio":{"data":"YWJj","format":"wav"}}`},
		{"pdf", `{"type":"file","file":{"filename":"one.pdf","file_data":"data:application/pdf;base64,JVBERg=="}}`, `{"type":"input_file","filename":"one.pdf","file_data":"data:application/pdf;base64,JVBERg=="}`},
		{"video", `{"type":"video_url","video_url":"https://files.example/one.mp4"}`, `{"type":"input_video","video_url":{"url":"https://files.example/one.mp4"}}`},
	} {
		for _, from := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses} {
			for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
				t.Run(fmt.Sprintf("%s/%s/%s", test.name, from, policy), func(t *testing.T) {
					var request any = &dto.GeneralOpenAIRequest{}
					body := fmt.Sprintf(`{"model":"test","messages":[{"role":"user","content":[%s]}]}`, test.chat)
					target, expected := types.RelayFormat(types.RelayFormatOpenAIResponses), test.responses
					if from == types.RelayFormatOpenAIResponses {
						request = &dto.OpenAIResponsesRequest{}
						body = fmt.Sprintf(`{"model":"test","input":[{"role":"user","content":[%s]}]}`, test.responses)
						target, expected = types.RelayFormatOpenAI, test.chat
					}
					require.NoError(t, kitutil.Unmarshal([]byte(body), request))
					requirements, err := AnalyzeRequest(request)
					require.NoError(t, err)
					plan, err := PlanConversion(requirements, target, policy)
					require.NoError(t, err)
					require.Empty(t, plan.Diagnostics)
					result, err := ExecuteConversionPlan(context.Background(), nil, plan, request)
					require.NoError(t, err)
					actual, err := kitutil.Marshal(mediaTestConvertedPart(t, result.Value))
					require.NoError(t, err)
					assert.JSONEq(t, expected, string(actual))
				})
			}
		}
	}
}

func TestMediaVideoExtensionsRequireExplicitLossPolicy(t *testing.T) {
	request := &dto.OpenAIResponsesRequest{Model: "test", Input: []byte(`[{"role":"user","content":[{"type":"input_video","video_url":{"url":"https://files.example/one.mp4","fps":24}}]}]`)}
	requirements, err := AnalyzeRequest(request)
	require.NoError(t, err)
	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		plan, err := PlanConversion(requirements, types.RelayFormatOpenAI, policy)
		require.NotEmpty(t, plan.Diagnostics)
		assert.Equal(t, "media_extension_not_portable", plan.Diagnostics[0].Code)
		if policy != types.ConversionLossPolicyAllow {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)
		result, err := ExecuteConversionPlan(context.Background(), nil, plan, request)
		require.NoError(t, err)
		assert.Equal(t, "https://files.example/one.mp4", mediaTestConvertedPart(t, result.Value)["video_url"])
	}
}

func TestGeminiInlineImageEncodingAcrossConvertedProtocols(t *testing.T) {
	SetMediaResolver(MediaResolver{GetBase64Data: func(_ context.Context, source types.FileSource, _ ...string) (string, string, error) {
		header, data, found := strings.Cut(source.GetRawData(), ",")
		require.True(t, found)
		assert.Equal(t, "data:image/png;base64", header)
		_, err := base64.StdEncoding.DecodeString(data)
		require.NoError(t, err)
		return data, "image/png", nil
	}})
	t.Cleanup(func() { SetMediaResolver(goldenMediaResolver()) })
	image := []byte{0xfb, 0xff}
	for _, input := range []string{base64.StdEncoding.EncodeToString(image), base64.URLEncoding.EncodeToString(image), base64.RawURLEncoding.EncodeToString(image), "not%base64"} {
		for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
			t.Run(input+"/"+string(target), func(t *testing.T) {
				request := &dto.GeminiChatRequest{}
				require.NoError(t, kitutil.Unmarshal([]byte(fmt.Sprintf(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":%q}}]}]}`, input)), request))
				meta := &convmeta.Values{UpstreamModelName: "test", ChannelMetaAttached: true, Options: &convmeta.Options{Claude: convmeta.ClaudeOptions{DefaultMaxTokens: func(string) int { return 32 }}}}
				result, err := ConvertRequest(context.Background(), meta, target, request)
				if input == "not%base64" {
					require.ErrorContains(t, err, "invalid Gemini inline image encoding")
					return
				}
				require.NoError(t, err)
				part := mediaTestConvertedPart(t, result.Value)
				var actual string
				switch target {
				case types.RelayFormatClaude:
					actual = part["source"].(map[string]any)["data"].(string)
				case types.RelayFormatOpenAI:
					actual = strings.TrimPrefix(part["image_url"].(map[string]any)["url"].(string), "data:image/png;base64,")
				default:
					actual = strings.TrimPrefix(part["image_url"].(string), "data:image/png;base64,")
				}
				assert.Equal(t, base64.StdEncoding.EncodeToString(image), actual)
			})
		}
	}
}

func mediaTestConvertedPart(t *testing.T, request any) map[string]any {
	t.Helper()
	encoded, err := kitutil.Marshal(request)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, kitutil.Unmarshal(encoded, &body))
	messages := body["messages"]
	if messages == nil {
		messages = body["input"]
	}
	items := messages.([]any)
	require.Len(t, items, 1)
	parts := items[0].(map[string]any)["content"].([]any)
	require.Len(t, parts, 1)
	return parts[0].(map[string]any)
}
