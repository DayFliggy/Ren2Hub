package cloudflare

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudflareAudioImplementedSurface(t *testing.T) {
	for _, test := range []struct {
		name, format  string
		mode          int
		stream, valid bool
	}{
		{"transcription_json", "json", relayconstant.RelayModeAudioTranscription, false, true},
		{"transcription_text", "text", relayconstant.RelayModeAudioTranscription, false, true},
		{"transcription_stream", "json", relayconstant.RelayModeAudioTranscription, true, false},
		{"timestamp_format", "verbose_json", relayconstant.RelayModeAudioTranscription, false, false},
		{"translation_is_not_transcription", "json", relayconstant.RelayModeAudioTranslation, false, false},
		{"speech_is_not_transcription", "json", relayconstant.RelayModeAudioSpeech, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var form bytes.Buffer
			writer := multipart.NewWriter(&form)
			part, err := writer.CreateFormFile("file", "audio.wav")
			require.NoError(t, err)
			_, err = part.Write([]byte("audio"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &form)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			info := &relaycommon.RelayInfo{RelayMode: test.mode}
			body, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{ResponseFormat: test.format, Stream: &test.stream})
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			assert.Equal(t, "audio", string(payload))
			assert.Equal(t, test.format, c.GetString("response_format"))
		})
	}
}
