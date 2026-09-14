package relay

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	geminitask "github.com/QuantumNous/new-api/relay/channel/task/gemini"
	vertextask "github.com/QuantumNous/new-api/relay/channel/task/vertex"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const veoFixturePNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/f3sAAAAASUVORK5CYII="

func TestVeoEffectiveRequestMatchesBilling(t *testing.T) {
	for _, provider := range []string{"gemini", "vertex"} {
		for _, tc := range []struct {
			name, fields, resolution string
			seconds                  int
			ratio                    float64
		}{
			{"default", ``, "720p", 8, 1},
			{"seconds", `,"seconds":"4"`, "720p", 4, 1},
			{"duration", `,"duration":6,"seconds":"4"`, "720p", 6, 1},
			{"metadata", `,"duration":4,"metadata":{"durationSeconds":8,"resolution":"4K","seed":0,"generateAudio":false}`, "4k", 8, 1.5},
			{"size", `,"size":"1920x1080"`, "1080p", 8, 1},
			{"metadata aliases", `,"metadata":{"DurationSeconds":4,"Resolution":"4K"}`, "4k", 4, 1.5},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				var adapter channel.TaskAdaptor = &geminitask.TaskAdaptor{}
				if provider == "vertex" {
					adapter = &vertextask.TaskAdaptor{}
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"veo-3.1-generate-preview","prompt":"fixture"`+tc.fields+`}`))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "veo-3.1-generate-preview"}}
				require.Nil(t, adapter.ValidateRequestAndSetAction(c, info))
				ratios := adapter.EstimateBilling(c, info)
				body, err := adapter.BuildRequestBody(c, info)
				require.NoError(t, err)
				wire, err := io.ReadAll(body)
				require.NoError(t, err)
				assert.Equal(t, tc.seconds, int(gjson.GetBytes(wire, "parameters.durationSeconds").Int()))
				assert.Equal(t, float64(tc.seconds), ratios["seconds"])
				assert.Equal(t, tc.resolution, gjson.GetBytes(wire, "parameters.resolution").String())
				assert.Equal(t, tc.ratio, ratios["resolution"])
				assert.EqualValues(t, 1, gjson.GetBytes(wire, "parameters.sampleCount").Int())
				if tc.name == "metadata" {
					assert.Equal(t, "0", gjson.GetBytes(wire, "parameters.seed").Raw)
					assert.Equal(t, "false", gjson.GetBytes(wire, "parameters.generateAudio").Raw)
				}
			})
		}
	}
}

func TestVeoInvalidInputsCannotBuildRequest(t *testing.T) {
	fields := []string{
		`,"seconds":"invalid"`, `,"seconds":"0"`, `,"seconds":"999999999999999999999999"`,
		`,"duration":1.5`, `,"duration":"invalid"`, `,"metadata":"invalid"`,
		`,"metadata":{"durationSeconds":0}`, `,"metadata":{"durationSeconds":-4}`, `,"metadata":{"durationSeconds":3601}`,
		`,"metadata":{"durationSeconds":1.5}`, `,"metadata":{"sampleCount":2}`, `,"metadata":{"resolution":"unknown"}`,
		`,"metadata":{"DurationSeconds":-1}`, `,"metadata":{"SampleCount":2}`, `,"metadata":{"Resolution":"invalid"}`,
		`,"metadata":{"DurationSeconds":8,"durationSeconds":4}`,
		`,"images":["invalid"]`, `,"input_reference":"https://image.invalid/input.png"`,
		`,"images":["data:image/png;base64,invalid"]`, `,"images":["data:image/png,not-base64"]`,
		`,"images":["","another"]`, `,"images":["` + base64.StdEncoding.EncodeToString([]byte("not an image")) + `"]`,
		`,"images":["` + veoFixturePNG + `"],"input_reference":"invalid"`,
		`,"images":["` + veoFixturePNG + `"],"image":"invalid"`,
	}
	for _, provider := range []string{"gemini", "vertex"} {
		for i, extra := range fields {
			t.Run(fmt.Sprintf("%s/%d", provider, i), func(t *testing.T) {
				var adapter channel.TaskAdaptor = &geminitask.TaskAdaptor{}
				if provider == "vertex" {
					adapter = &vertextask.TaskAdaptor{}
				}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"model":"veo-3.1-generate-preview","prompt":"fixture"`+extra+`}`))
				c.Request.Header.Set("Content-Type", "application/json")
				t.Cleanup(func() { common.CleanupBodyStorage(c) })
				info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{}}
				err := adapter.ValidateRequestAndSetAction(c, info)
				require.NotNil(t, err)
				assert.Equal(t, http.StatusBadRequest, err.StatusCode)
				assert.True(t, err.LocalError)
				_, bodyErr := adapter.BuildRequestBody(c, info)
				assert.Error(t, bodyErr)
			})
		}
	}
}

func TestVeoImageInputAndMultipartReadFailure(t *testing.T) {
	for _, input := range []string{veoFixturePNG, "data:image/png;base64," + veoFixturePNG} {
		image, err := geminitask.ParseImageInput(input)
		require.NoError(t, err)
		assert.Equal(t, veoFixturePNG, image.BytesBase64Encoded)
		assert.Equal(t, "image/png", image.MimeType)
	}
	_, err := geminitask.ParseImageInput("data:image/jpeg;base64," + veoFixturePNG)
	assert.Error(t, err)
	_, err = geminitask.ParseImageInput(strings.Repeat("A", base64.StdEncoding.EncodedLen(20*1024*1024)+4))
	assert.Error(t, err)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("input_reference", "input.png")
	require.NoError(t, err)
	image, err := base64.StdEncoding.DecodeString(veoFixturePNG)
	require.NoError(t, err)
	_, err = part.Write(image)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, c.Request.ParseMultipartForm(1))
	_, err = geminitask.ExtractMultipartImage(c)
	require.NoError(t, err)
	require.NoError(t, c.Request.MultipartForm.RemoveAll())
	_, err = geminitask.ExtractMultipartImage(c)
	assert.Error(t, err, "a removed upload cannot become a request without the image")
}
