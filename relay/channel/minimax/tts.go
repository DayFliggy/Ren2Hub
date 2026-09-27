package minimax

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type MiniMaxTTSRequest struct {
	Model             string             `json:"model"`
	Text              string             `json:"text"`
	Stream            *bool              `json:"stream,omitempty"`
	StreamOptions     *StreamOptions     `json:"stream_options,omitempty"`
	VoiceSetting      VoiceSetting       `json:"voice_setting"`
	PronunciationDict *PronunciationDict `json:"pronunciation_dict,omitempty"`
	AudioSetting      *AudioSetting      `json:"audio_setting,omitempty"`
	TimbreWeights     []TimbreWeight     `json:"timbre_weights,omitempty"`
	LanguageBoost     string             `json:"language_boost,omitempty"`
	VoiceModify       *VoiceModify       `json:"voice_modify,omitempty"`
	SubtitleEnable    *bool              `json:"subtitle_enable,omitempty"`
	OutputFormat      string             `json:"output_format,omitempty"`
	AigcWatermark     *bool              `json:"aigc_watermark,omitempty"`
}

type StreamOptions struct {
	ExcludeAggregatedAudio *bool `json:"exclude_aggregated_audio,omitempty"`
}

type VoiceSetting struct {
	VoiceID           string   `json:"voice_id"`
	Speed             *float64 `json:"speed,omitempty"`
	Vol               *float64 `json:"vol,omitempty"`
	Pitch             *int     `json:"pitch,omitempty"`
	Emotion           string   `json:"emotion,omitempty"`
	TextNormalization *bool    `json:"text_normalization,omitempty"`
	LatexRead         *bool    `json:"latex_read,omitempty"`
}

type PronunciationDict struct {
	Tone []string `json:"tone,omitempty"`
}

type AudioSetting struct {
	SampleRate *int   `json:"sample_rate,omitempty"`
	Bitrate    *int   `json:"bitrate,omitempty"`
	Format     string `json:"format,omitempty"`
	Channel    *int   `json:"channel,omitempty"`
	ForceCbr   *bool  `json:"force_cbr,omitempty"`
}

type TimbreWeight struct {
	VoiceID string `json:"voice_id"`
	Weight  int    `json:"weight"`
}

type VoiceModify struct {
	Pitch        *int   `json:"pitch,omitempty"`
	Intensity    *int   `json:"intensity,omitempty"`
	Timbre       *int   `json:"timbre,omitempty"`
	SoundEffects string `json:"sound_effects,omitempty"`
}

type MiniMaxTTSResponse struct {
	Data      MiniMaxTTSData   `json:"data"`
	ExtraInfo MiniMaxExtraInfo `json:"extra_info"`
	TraceID   string           `json:"trace_id"`
	BaseResp  MiniMaxBaseResp  `json:"base_resp"`
}

type MiniMaxTTSData struct {
	Audio  string `json:"audio"`
	Status int    `json:"status"`
}

type MiniMaxExtraInfo struct {
	UsageCharacters *int64 `json:"usage_characters"`
}

type MiniMaxBaseResp struct {
	StatusCode int64  `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

func getContentTypeByFormat(format string) string {
	contentTypeMap := map[string]string{
		"mp3":  "audio/mpeg",
		"wav":  "audio/wav",
		"flac": "audio/flac",
		"pcm":  "audio/pcm",
	}
	if ct, ok := contentTypeMap[format]; ok {
		return ct
	}
	return "audio/mpeg" // default to mp3
}

func handleTTSResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid MiniMax audio response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("failed to read minimax response: %w", readErr),
			types.ErrorCodeReadResponseBodyFailed,
			http.StatusBadGateway,
		)
	}

	// Parse response
	var minimaxResp MiniMaxTTSResponse
	if unmarshalErr := common.Unmarshal(body, &minimaxResp); unmarshalErr != nil {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("failed to unmarshal minimax TTS response: %w", unmarshalErr),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}

	// Check base_resp status code
	if minimaxResp.BaseResp.StatusCode != 0 {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("minimax TTS error: %d - %s", minimaxResp.BaseResp.StatusCode, minimaxResp.BaseResp.StatusMsg),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}

	// Check if we have audio data
	if minimaxResp.Data.Audio == "" {
		return nil, types.NewErrorWithStatusCode(
			fmt.Errorf("no audio data in minimax TTS response"),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}
	if minimaxResp.Data.Status != 2 {
		return nil, types.NewOpenAIError(fmt.Errorf("MiniMax speech did not complete"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	characters := minimaxResp.ExtraInfo.UsageCharacters
	if characters != nil && (*characters < 0 || *characters > math.MaxInt32) {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid MiniMax character usage"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	audioData, decodeErr := hex.DecodeString(minimaxResp.Data.Audio)
	if decodeErr != nil || len(audioData) == 0 {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid MiniMax hex audio"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	contentType := getContentTypeByFormat(c.GetString("response_format"))
	if info.IsStream {
		delta, marshalErr := common.Marshal(map[string]string{"type": "speech.audio.delta", "audio": base64.StdEncoding.EncodeToString(audioData)})
		if marshalErr != nil {
			return nil, types.NewOpenAIError(marshalErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		audioData = []byte("data: " + string(delta) + "\n\ndata: {\"type\":\"speech.audio.done\"}\n\n")
		contentType = "text/event-stream"
	}
	openai.OpenaiTTSHandler(c, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(audioData))}, info)
	observed := &dto.Usage{UsageSource: "missing"}
	if info.HasValidOutput() && characters != nil {
		observed.UsageSource = "provider_characters"
		observed.MeasuredQuantities = map[string]string{"characters": strconv.FormatInt(*characters, 10)}
	}
	return observed, helper.StreamError(info)
}

func handleChatCompletionResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("failed to read minimax response"),
			types.ErrorCodeReadResponseBodyFailed,
			http.StatusInternalServerError,
		)
	}
	defer resp.Body.Close()

	// Set response headers
	for key, values := range resp.Header {
		if !service.ShouldCopyUpstreamHeader(c, key, values) {
			continue
		}
		for _, value := range values {
			c.Header(key, value)
		}
	}

	c.Data(resp.StatusCode, "application/json", body)
	return nil, nil
}
