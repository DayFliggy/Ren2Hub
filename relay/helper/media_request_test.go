package helper

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAudioMultipartStreamPlanning(t *testing.T) {
	for _, stream := range []string{"true", "false", "invalid", ""} {
		t.Run(stream, func(t *testing.T) {
			var payload bytes.Buffer
			writer := multipart.NewWriter(&payload)
			require.NoError(t, writer.WriteField("model", "gpt-4o-transcribe"))
			if stream != "" {
				require.NoError(t, writer.WriteField("stream", stream))
			}
			require.NoError(t, writer.Close())
			original := payload.String()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", &payload)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			t.Cleanup(func() { common.CleanupBodyStorage(c) })
			request, err := GetAndValidAudioRequest(c, relayconstant.RelayModeAudioTranscription)
			if stream == "invalid" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, stream == "true", request.IsStream(c.Request))
			assert.Equal(t, stream != "", request.Stream != nil)
			replayed, err := io.ReadAll(c.Request.Body)
			require.NoError(t, err)
			assert.Equal(t, original, string(replayed))
		})
	}
}

func TestVectorNullInputReturnsValidationError(t *testing.T) {
	for _, format := range []types.RelayFormat{types.RelayFormatEmbedding, types.RelayFormatRerank} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", strings.NewReader("null"))
		c.Request.Header.Set("Content-Type", "application/json")
		t.Cleanup(func() { common.CleanupBodyStorage(c) })
		_, err := GetAndValidateRequest(c, format)
		require.Error(t, err)
	}
}
