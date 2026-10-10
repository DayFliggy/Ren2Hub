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
)

// A converted Chat image_url object carries only url and detail; the MIME type
// the converter knows stays in memory for later steps and token counting
// instead of reaching the upstream as a stray MimeType key (NewAPI 0a03088e0).
func TestConvertedChatImageURLCarriesOnlyChatFields(t *testing.T) {
	claude := &dto.ClaudeRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"m","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},{"type":"text","text":"what"}]}]}`), claude))
	gemini := &dto.GeminiChatRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"iVBORw0KGgo="}},{"fileData":{"mimeType":"image/jpeg","fileUri":"https://example.com/a.jpg"}},{"text":"what"}]}]}`), gemini))

	for name, request := range map[string]any{"claude": claude, "gemini": gemini} {
		t.Run(name, func(t *testing.T) {
			info := &convmeta.Values{Options: &convmeta.Options{ConversionPolicy: types.ConversionLossPolicySafe}, UpstreamModelName: "m", ChannelMetaAttached: true}
			result, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, request)
			require.NoError(t, err)
			chat, ok := result.Value.(*dto.GeneralOpenAIRequest)
			require.True(t, ok)

			encoded := marshaledField(t, chat)
			assert.NotContains(t, encoded, "MimeType")
			var decoded struct {
				Messages []struct {
					Content []map[string]any `json:"content"`
				} `json:"messages"`
			}
			require.NoError(t, kitutil.Unmarshal([]byte(encoded), &decoded))
			images := 0
			for _, message := range decoded.Messages {
				for _, part := range message.Content {
					imageURL, isImage := part["image_url"].(map[string]any)
					if !isImage {
						continue
					}
					images++
					for key := range imageURL {
						assert.Contains(t, []string{"url", "detail"}, key)
					}
				}
			}
			assert.Positive(t, images)
		})
	}

	result, err := ConvertRequest(context.Background(), &convmeta.Values{UpstreamModelName: "m", ChannelMetaAttached: true}, types.RelayFormatOpenAI, gemini)
	require.NoError(t, err)
	chat := result.Value.(*dto.GeneralOpenAIRequest)
	var mimeTypes []string
	for _, part := range chat.Messages[0].ParseContent() {
		if image := part.GetImageMedia(); image != nil {
			mimeTypes = append(mimeTypes, image.MimeType)
		}
	}
	assert.Equal(t, []string{"image/png", "image/jpeg"}, mimeTypes)
}
