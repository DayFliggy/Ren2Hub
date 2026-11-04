package dto

import (
	"fmt"
	"testing"

	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiArgonFieldsPreserveNativeValuesAndAliases(t *testing.T) {
	for _, names := range [][2]string{{"continuationToken", "mediaProcessing"}, {"continuation_token", "media_processing"}} {
		t.Run(names[0], func(t *testing.T) {
			body := fmt.Sprintf(`{"%s":{"opaque":[1,"next"]},"contents":[{"role":"user","parts":[{"%s":{"video":{"fps":0}}}]}],"generationConfig":{"%s":{"audio":false}}}`, names[0], names[1], names[1])
			var request GeminiChatRequest
			require.NoError(t, kitutil.Unmarshal([]byte(body), &request))
			assert.JSONEq(t, `{"opaque":[1,"next"]}`, string(request.ContinuationToken))
			require.Len(t, request.Contents, 1)
			assert.JSONEq(t, `{"video":{"fps":0}}`, string(request.Contents[0].Parts[0].MediaProcessing))
			assert.JSONEq(t, `{"audio":false}`, string(request.GenerationConfig.MediaProcessing))
			encoded, err := kitutil.Marshal(request)
			require.NoError(t, err)
			assert.Contains(t, string(encoded), `"continuationToken"`)
			assert.Contains(t, string(encoded), `"mediaProcessing"`)
			var roundTrip GeminiChatRequest
			require.NoError(t, kitutil.Unmarshal(encoded, &roundTrip))
			assert.Equal(t, request, roundTrip)
		})
	}
}
