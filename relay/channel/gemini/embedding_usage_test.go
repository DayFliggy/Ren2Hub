package gemini

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiEmbeddingUsageMetadataAcrossProtocols(t *testing.T) {
	for _, route := range []string{"native-single", "native-batch", "openai"} {
		for _, tc := range []struct {
			name, metadata string
			want           int
		}{
			{"reported", `,"usageMetadata":{"promptTokenCount":120,"promptTokenDetails":[{"modality":"TEXT","tokenCount":30},{"modality":"IMAGE","tokenCount":40},{"modality":"AUDIO","tokenCount":50}]}`, 120},
			{"missing", "", 19},
			{"zero", `,"usageMetadata":{"promptTokenCount":0}`, 19},
			{"null", `,"usageMetadata":null`, 19},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/embeddings", nil)
				info := &relaycommon.RelayInfo{IsGeminiBatchEmbedding: route == "native-batch", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "embedding-test"}}
				info.SetEstimatePromptTokens(19)
				embedding := `"embeddings":[{"values":[0.25,0.75]}]`
				if route == "native-single" {
					embedding = `"embedding":{"values":[0.25,0.75]}`
				}
				body := fmt.Sprintf("{%s%s}", embedding, tc.metadata)
				response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
				var usage *dto.Usage
				var apiErr *types.NewAPIError
				if route == "openai" {
					usage, apiErr = GeminiEmbeddingHandler(ctx, info, response)
				} else {
					usage, apiErr = NativeGeminiEmbeddingHandler(ctx, response, info)
				}
				require.Nil(t, apiErr)
				require.NotNil(t, usage)
				assert.Equal(t, tc.want, usage.PromptTokens)
				assert.Equal(t, tc.want, usage.TotalTokens)
				assert.Zero(t, usage.CompletionTokens)
				if tc.name == "reported" {
					assert.Equal(t, 30, usage.PromptTokensDetails.TextTokens)
					assert.Equal(t, 40, usage.PromptTokensDetails.ImageTokens)
					assert.Equal(t, 50, usage.PromptTokensDetails.AudioTokens)
				}
				if route == "openai" {
					var converted dto.OpenAIEmbeddingResponse
					require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &converted))
					require.Len(t, converted.Data, 1)
					assert.Equal(t, []float64{0.25, 0.75}, converted.Data[0].Embedding)
					assert.Equal(t, tc.want, converted.Usage.PromptTokens)
				} else {
					assert.Equal(t, body, recorder.Body.String(), "native response bytes must remain intact")
				}
			})
		}
	}
}
