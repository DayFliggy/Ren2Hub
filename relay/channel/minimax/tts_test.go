package minimax

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMiniMaxTTSRequestContract(t *testing.T) {
	zero := 0.0
	for _, test := range []struct {
		name, format, metadata string
		speed                  *float64
		valid                  bool
	}{
		{"default_format", "", "", nil, true},
		{"zero_speed", "wav", "", &zero, true},
		{"metadata_model_cannot_override_mapping", "pcm", `{"model":"other"}`, nil, true},
		{"url_delivery_is_not_audio", "mp3", `{"output_format":"url"}`, nil, false},
		{"provider_stream_not_implemented", "mp3", `{"stream":true}`, nil, false},
		{"metadata_format_cannot_override_client", "wav", `{"audio_setting":{"format":"mp3"}}`, nil, false},
		{"missing_audio_setting", "mp3", `{"audio_setting":null}`, nil, false},
		{"unsupported_format", "opus", "", nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "mapped-speech"}, RelayMode: relayconstant.RelayModeAudioSpeech, OriginModelName: "alias"}
			body, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{Model: "alias", Input: "hello", Voice: "voice", Speed: test.speed, ResponseFormat: test.format, Metadata: []byte(test.metadata)})
			if !test.valid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			payload, err := io.ReadAll(body)
			require.NoError(t, err)
			var converted MiniMaxTTSRequest
			require.NoError(t, common.Unmarshal(payload, &converted))
			assert.Equal(t, "mapped-speech", converted.Model)
			assert.Equal(t, "hex", converted.OutputFormat)
			format := test.format
			if format == "" {
				format = "mp3"
			}
			assert.Equal(t, format, converted.AudioSetting.Format)
			assert.Equal(t, format, c.GetString("response_format"))
			if test.speed != nil {
				require.NotNil(t, converted.VoiceSetting.Speed)
				assert.Equal(t, 0.0, *converted.VoiceSetting.Speed)
				assert.Contains(t, string(payload), `"speed":0`)
			} else {
				assert.NotContains(t, string(payload), `"speed"`)
			}
		})
	}
}

func TestMiniMaxTTSRejectsIncompleteAndUnsafeUsage(t *testing.T) {
	for _, payload := range []string{
		`{"data":{"audio":"0001","status":1}}`,
		`{"data":{"audio":"https://example.invalid/audio","status":2}}`,
		`{"data":{"audio":"0001","status":2},"extra_info":{"usage_characters":2147483648}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, RelayMode: relayconstant.RelayModeAudioSpeech}
			_, apiErr := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, info)
			require.NotNil(t, apiErr)
			assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
			assert.False(t, info.HasValidOutput())
			assert.Empty(t, recorder.Body.String())
		})
	}
}

func TestMiniMaxTTSUsageDistinguishesMissingFromExplicitZero(t *testing.T) {
	for _, test := range []struct {
		extra   string
		present bool
	}{
		{`{}`, false}, {`{"usage_characters":null}`, false}, {`{"usage_characters":0}`, true},
	} {
		t.Run(test.extra, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, RelayMode: relayconstant.RelayModeAudioSpeech}
			payload := `{"data":{"audio":"0001","status":2},"extra_info":` + test.extra + `}`
			value, apiErr := handleTTSResponse(c, &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(payload))}, info)
			require.Nil(t, apiErr)
			usage := value.(*dto.Usage)
			count, present := usage.MeasuredQuantities["characters"]
			require.Equal(t, test.present, present)
			if present {
				require.Equal(t, "0", count)
			}
		})
	}
}

func TestMiniMaxTTSMetadataPreservesFalseAndZero(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "speech"}, RelayMode: relayconstant.RelayModeAudioSpeech}
	body, err := (&Adaptor{}).ConvertAudioRequest(c, info, dto.AudioRequest{Input: "hello", Voice: "voice", Metadata: []byte(`{"stream":false,"aigc_watermark":false,"voice_setting":{"pitch":0,"text_normalization":false},"audio_setting":{"force_cbr":false},"voice_modify":{"intensity":0}}`)})
	require.NoError(t, err)
	var converted MiniMaxTTSRequest
	require.NoError(t, common.DecodeJson(body, &converted))
	require.NotNil(t, converted.Stream)
	assert.False(t, *converted.Stream)
	require.NotNil(t, converted.AigcWatermark)
	assert.False(t, *converted.AigcWatermark)
	require.NotNil(t, converted.VoiceSetting.Pitch)
	assert.Zero(t, *converted.VoiceSetting.Pitch)
	require.NotNil(t, converted.VoiceSetting.TextNormalization)
	assert.False(t, *converted.VoiceSetting.TextNormalization)
	require.NotNil(t, converted.AudioSetting.ForceCbr)
	assert.False(t, *converted.AudioSetting.ForceCbr)
	require.NotNil(t, converted.VoiceModify.Intensity)
	assert.Zero(t, *converted.VoiceModify.Intensity)
}
