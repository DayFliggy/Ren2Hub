package helper

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFinalImagePriceUsesWireQuantities(t *testing.T) {
	for _, test := range []struct {
		name, body string
		api, count int
	}{
		{"openai", `{"n":3}`, constant.APITypeOpenAI, 3},
		{"default", `{}`, constant.APITypeOpenAI, 1},
		{"zero", `{"n":0}`, constant.APITypeOpenAI, 0},
		{"null", `{"n":null}`, constant.APITypeOpenAI, 0},
		{"negative", `{"n":-1}`, constant.APITypeOpenAI, 0},
		{"overflow", `{"n":18446744073709551615}`, constant.APITypeOpenAI, 0},
		{"fraction", `{"n":1.5}`, constant.APITypeOpenAI, 0},
		{"ali", `{"parameters":{"n":4}}`, constant.APITypeAli, 4},
		{"ali_missing_wire_parameters", `{"n":4,"size":"1024x1024"}`, constant.APITypeAli, 0},
		{"ali_missing", `{"parameters":{}}`, constant.APITypeAli, 0},
		{"imagen", `{"parameters":{"sampleCount":3}}`, constant.APITypeGemini, 3},
		{"imagen_invalid", `{"parameters":{"sampleCount":129}}`, constant.APITypeVertexAi, 0},
		{"siliconflow", `{"batch_size":6}`, constant.APITypeSiliconFlow, 6},
		{"siliconflow_invalid", `{"batch_size":129}`, constant.APITypeSiliconFlow, 0},
		{"replicate", `{"input":{"num_outputs":8}}`, constant.APITypeReplicate, 8},
		{"replicate_invalid", `{"input":{"num_outputs":129}}`, constant.APITypeReplicate, 0},
		{"minimax", `{"n":2}`, constant.APITypeMiniMax, 2},
		{"volc_batch", `{"n":1,"sequential_image_generation_options":{"max_images":5}}`, constant.APITypeVolcEngine, 5},
		{"jimeng_single", `{"n":3}`, constant.APITypeJimeng, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: test.api}, RelayFormat: types.RelayFormatOpenAIImage}
			meta, err := FinalImagePriceMeta(c, info, strings.NewReader(test.body))
			if test.count == 0 {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprint(test.count), meta.BillingQuantities["n"])
		})
	}
}

func TestFinalImagePriceReadsMultipartAndQuality(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("n", "3"))
		require.NoError(t, writer.WriteField("size", "1792x1024"))
		require.NoError(t, writer.WriteField("quality", "hd"))
		if duplicate {
			require.NoError(t, writer.WriteField("n", "128"))
		}
		file, err := writer.CreateFormFile("image", "image.png")
		require.NoError(t, err)
		_, err = file.Write([]byte("file content does not affect quantity"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/images/edits", nil)
		c.Request.Header.Set("Content-Type", writer.FormDataContentType())
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI}, OriginModelName: "dall-e-3"}
		meta, err := FinalImagePriceMeta(c, info, &body)
		if duplicate {
			require.ErrorContains(t, err, "duplicate")
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, "3", meta.BillingQuantities["n"])
	}
}

func TestFinalImagePricePreservesSentTier(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI}, RelayFormat: types.RelayFormatOpenAIImage}
	_, err := FinalImagePriceMeta(c, info, strings.NewReader(`{"n":2,"service_tier":"priority","quality":"hd"}`))
	require.NoError(t, err)
	require.Equal(t, "priority", info.BillingRequirements.ServiceTier)
	require.Equal(t, "priority", info.BillingRequestInput.Attributes["service_tier"])
}
