package relay

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/relay/channel/ali"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageAdapterDeliveryContracts(t *testing.T) {
	for _, provider := range []struct {
		name, model, valid, empty string
		channel                   int
	}{
		{"openai", "gpt-image-1", `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`, `{"data":[]}`, constant.ChannelTypeOpenAI},
		{"minimax", "image-01", `{"data":{"image_base64":["first","second"]}}`, `{"data":{}}`, constant.ChannelTypeMiniMax},
		{"imagen", "imagen-4", `{"predictions":[{"bytesBase64Encoded":"first"},{"bytesBase64Encoded":"second"}]}`, `{"predictions":[{"raiFilteredReason":"filtered"}]}`, constant.ChannelTypeGemini},
		{"ali_sync", "qwen-image", `{"output":{"results":[{"b64_image":"first"},{"b64_image":"second"}]}}`, `{"output":{"results":[]}}`, constant.ChannelTypeAli},
		{"jimeng", "jimeng", `{"code":10000,"data":{"binary_data_base64":["first","second"]}}`, `{"code":10000,"data":{}}`, constant.ChannelTypeJimeng},
		{"replicate", "model/image", `{"status":"succeeded","output":["https://example.invalid/first.png","https://example.invalid/second.png"]}`, `{"status":"succeeded","output":[]}`, constant.ChannelTypeReplicate},
		{"zhipu", "cogview-4", `{"data":[{"url":"https://example.invalid/1.png","b64_json":"first"},{"url":"https://example.invalid/2.png","b64_json":"second"}]}`, `{"data":[]}`, constant.ChannelTypeZhipu_v4},
		{"xai", "grok-imagine-image", `{"data":[{"b64_json":"first"},{"b64_json":"second"}]}`, `{"data":[]}`, constant.ChannelTypeXai},
	} {
		for _, stream := range []bool{false, true} {
			for _, valid := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/stream_%v/valid_%v", provider.name, stream, valid), func(t *testing.T) {
					api, ok := common.ChannelType2APIType(provider.channel)
					require.True(t, ok)
					adaptor := GetAdaptor(api)
					require.NotNil(t, adaptor)
					recorder := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(recorder)
					c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
					info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: api, ChannelType: provider.channel, UpstreamModelName: provider.model},
						RelayFormat: types.RelayFormatOpenAIImage, RelayMode: relayconstant.RelayModeImagesGenerations, IsStream: stream,
						StartTime: time.Now(), Request: &dto.ImageRequest{Model: provider.model}}
					info.PriceData.UsePrice = true
					info.PriceData.Usage = &billing.Usage{Quantities: map[string]string{"images": "3"}}
					adaptor.Init(info)
					if aliAdaptor, ok := adaptor.(*ali.Adaptor); ok {
						aliAdaptor.IsSyncImageModel = true
					}
					body := provider.empty
					if valid {
						body = provider.valid
					}
					response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
					usage, apiErr := adaptor.DoResponse(c, response, info)
					if !valid {
						require.NotNil(t, apiErr)
						assert.False(t, info.HasValidOutput())
						assert.False(t, c.Writer.Written())
						assert.Empty(t, recorder.Body.String())
						return
					}
					require.Nil(t, apiErr)
					require.NotNil(t, usage)
					assert.True(t, info.HasValidOutput())
					assert.Equal(t, "2", info.PriceData.Usage.Quantities["images"])
					if stream {
						assert.Equal(t, 2, strings.Count(recorder.Body.String(), "event: image_generation.completed"))
						assert.Equal(t, 1, strings.Count(recorder.Body.String(), "data: [DONE]"))
					} else {
						var decoded dto.ImageResponse
						require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &decoded))
						assert.Len(t, decoded.Data, 2)
					}
				})
			}
		}
	}
}
