package router

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// Responses function_call_output media reaches a real Gemini channel as
// inline data in call order. Media Gemini cannot carry follows the conversion
// policy: safe and strict reject before any upstream request or charge, allow
// sends the tool result without the media.
func TestGatewayResponsesToolMediaOnGemini(t *testing.T) {
	const image = `{"type":"input_image","image_url":"data:image/png;base64,iVBORw0KGgo="}`
	const archive = `{"type":"input_file","filename":"logs.zip","file_data":"data:application/zip;base64,UEsDBA=="}`
	for _, policy := range []string{"safe", "strict", "allow"} {
		for _, rejected := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/reject=%t", policy, rejected), func(t *testing.T) {
				var sends atomic.Int32
				var upstream []byte
				base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
					sends.Add(1)
					payload, err := io.ReadAll(r.Body)
					require.NoError(t, err)
					upstream = payload
					w.Header().Set("Content-Type", "application/json")
					_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":1,"totalTokenCount":4}}`)
				})
				useGeminiSignalChannel(t, 0)
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Update("conversion_policy", policy).Error)
				media := image
				if rejected {
					media = archive
				}
				body := `{"model":"gemini-2.5-flash","max_output_tokens":10,"input":[` +
					`{"type":"function_call","call_id":"call_a","name":"shot","arguments":"{}"},` +
					`{"type":"function_call_output","call_id":"call_a","output":[{"type":"input_text","text":"first"},` + media + `]},` +
					`{"role":"user","content":"describe"}]}`
				request, err := http.NewRequest(http.MethodPost, base+"/v1/responses", strings.NewReader(body))
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+key)
				request.Header.Set("Content-Type", "application/json")
				response, err := http.DefaultClient.Do(request)
				require.NoError(t, err)
				defer response.Body.Close()
				output, err := io.ReadAll(response.Body)
				require.NoError(t, err)

				var user model.User
				if rejected && policy != "allow" {
					assert.Equal(t, http.StatusBadRequest, response.StatusCode, "%s", output)
					assert.Zero(t, sends.Load(), "unrepresentable tool media must not open an upstream request")
					awaitGatewayPermitsReleased(t)
					require.NoError(t, model.DB.First(&user, 1).Error)
					assert.Zero(t, user.UsedQuota)
					assert.Zero(t, user.ReservedQuota)
					return
				}
				require.Equal(t, http.StatusOK, response.StatusCode, "%s", output)
				require.EqualValues(t, 1, sends.Load())
				parts := gjson.GetBytes(upstream, "contents.#.parts|@flatten")
				assert.True(t, strings.Contains(parts.Raw, `"functionResponse"`), "%s", upstream)
				assert.Equal(t, "first", gjson.GetBytes(upstream, `contents.#.parts|@flatten|#(functionResponse).functionResponse.response.content`).String(), "%s", upstream)
				assert.NotContains(t, string(upstream), "[image]", "media must never be stringified")
				if rejected {
					assert.False(t, gjson.GetBytes(upstream, `contents.#.parts|@flatten|#(inlineData)`).Exists(), "allow sends the result without the omitted media: %s", upstream)
					assert.NotContains(t, string(upstream), "UEsDBA==", "omitted media must not reach a text field")
				} else {
					assert.Equal(t, "image/png", gjson.GetBytes(upstream, `contents.#.parts|@flatten|#(inlineData).inlineData.mimeType`).String(), "%s", upstream)
				}
				awaitGatewayPermitsReleased(t)
				require.Eventually(t, func() bool {
					return model.DB.First(&user, 1).Error == nil && user.UsedQuota > 0 && user.ReservedQuota == 0
				}, 2*time.Second, 10*time.Millisecond)
			})
		}
	}
}
