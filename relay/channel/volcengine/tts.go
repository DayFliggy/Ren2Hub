package volcengine

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type VolcengineTTSRequest struct {
	App     VolcengineTTSApp     `json:"app"`
	User    VolcengineTTSUser    `json:"user"`
	Audio   VolcengineTTSAudio   `json:"audio"`
	Request VolcengineTTSReqInfo `json:"request"`
}

type VolcengineTTSApp struct {
	AppID   string `json:"appid"`
	Token   string `json:"token"`
	Cluster string `json:"cluster"`
}

type VolcengineTTSUser struct {
	UID string `json:"uid"`
}

type VolcengineTTSAudio struct {
	VoiceType        string   `json:"voice_type"`
	Encoding         string   `json:"encoding"`
	SpeedRatio       float64  `json:"speed_ratio"`
	Rate             int      `json:"rate"`
	Bitrate          *int     `json:"bitrate,omitempty"`
	LoudnessRatio    *float64 `json:"loudness_ratio,omitempty"`
	EnableEmotion    *bool    `json:"enable_emotion,omitempty"`
	Emotion          string   `json:"emotion,omitempty"`
	EmotionScale     *float64 `json:"emotion_scale,omitempty"`
	ExplicitLanguage string   `json:"explicit_language,omitempty"`
	ContextLanguage  string   `json:"context_language,omitempty"`
}

type VolcengineTTSReqInfo struct {
	ReqID           string                   `json:"reqid"`
	Text            string                   `json:"text"`
	Operation       string                   `json:"operation"`
	Model           string                   `json:"model,omitempty"`
	TextType        string                   `json:"text_type,omitempty"`
	SilenceDuration *float64                 `json:"silence_duration,omitempty"`
	WithTimestamp   interface{}              `json:"with_timestamp,omitempty"`
	ExtraParam      *VolcengineTTSExtraParam `json:"extra_param,omitempty"`
}

type VolcengineTTSExtraParam struct {
	DisableMarkdownFilter      *bool                     `json:"disable_markdown_filter,omitempty"`
	EnableLatexTn              *bool                     `json:"enable_latex_tn,omitempty"`
	MuteCutThreshold           string                    `json:"mute_cut_threshold,omitempty"`
	MuteCutRemainMs            string                    `json:"mute_cut_remain_ms,omitempty"`
	DisableEmojiFilter         *bool                     `json:"disable_emoji_filter,omitempty"`
	UnsupportedCharRatioThresh *float64                  `json:"unsupported_char_ratio_thresh,omitempty"`
	AigcWatermark              *bool                     `json:"aigc_watermark,omitempty"`
	CacheConfig                *VolcengineTTSCacheConfig `json:"cache_config,omitempty"`
}

type VolcengineTTSCacheConfig struct {
	TextType *int  `json:"text_type,omitempty"`
	UseCache *bool `json:"use_cache,omitempty"`
}

type VolcengineTTSResponse struct {
	ReqID    string                     `json:"reqid"`
	Code     int                        `json:"code"`
	Message  string                     `json:"message"`
	Sequence int                        `json:"sequence"`
	Data     string                     `json:"data"`
	Addition *VolcengineTTSAdditionInfo `json:"addition,omitempty"`
}

type VolcengineTTSAdditionInfo struct {
	Duration string `json:"duration"`
}

var openAIToVolcengineVoiceMap = map[string]string{
	"alloy":   "zh_male_M392_conversation_wvae_bigtts",
	"echo":    "zh_male_wenhao_mars_bigtts",
	"fable":   "zh_female_tianmei_mars_bigtts",
	"onyx":    "zh_male_zhibei_mars_bigtts",
	"nova":    "zh_female_shuangkuaisisi_mars_bigtts",
	"shimmer": "zh_female_cancan_mars_bigtts",
}

var responseFormatToEncodingMap = map[string]string{
	"mp3":  "mp3",
	"opus": "ogg_opus",
	"aac":  "mp3",
	"flac": "mp3",
	"wav":  "wav",
	"pcm":  "pcm",
}

func parseVolcengineAuth(apiKey string) (appID, token string, err error) {
	parts := strings.Split(apiKey, "|")
	if len(parts) != 2 {
		return "", "", errors.New("invalid api key format, expected: appid|access_token")
	}
	return parts[0], parts[1], nil
}

func mapVoiceType(openAIVoice string) string {
	if voice, ok := openAIToVolcengineVoiceMap[openAIVoice]; ok {
		return voice
	}
	return openAIVoice
}

func mapEncoding(responseFormat string) string {
	if encoding, ok := responseFormatToEncodingMap[responseFormat]; ok {
		return encoding
	}
	return "mp3"
}

func getContentTypeByEncoding(encoding string) string {
	contentTypeMap := map[string]string{
		"mp3":      "audio/mpeg",
		"ogg_opus": "audio/ogg",
		"wav":      "audio/wav",
		"pcm":      "audio/pcm",
	}
	if ct, ok := contentTypeMap[encoding]; ok {
		return ct
	}
	return "application/octet-stream"
}

func handleTTSResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, encoding string) (usage any, err *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(errors.New("invalid VolcEngine audio response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("failed to read volcengine response"),
			types.ErrorCodeReadResponseBodyFailed,
			http.StatusBadGateway,
		)
	}

	var volcResp VolcengineTTSResponse
	if unmarshalErr := common.Unmarshal(body, &volcResp); unmarshalErr != nil {
		return nil, types.NewErrorWithStatusCode(
			errors.New("failed to parse volcengine response"),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}

	if volcResp.Code != 3000 {
		return nil, types.NewErrorWithStatusCode(
			errors.New(volcResp.Message),
			types.ErrorCodeBadResponse,
			http.StatusBadGateway,
		)
	}

	audioData, decodeErr := base64.StdEncoding.DecodeString(volcResp.Data)
	if decodeErr != nil || len(audioData) == 0 {
		return nil, types.NewErrorWithStatusCode(
			errors.New("failed to decode audio data"),
			types.ErrorCodeBadResponseBody,
			http.StatusBadGateway,
		)
	}

	contentType := getContentTypeByEncoding(encoding)
	if info.IsStream {
		delta, marshalErr := common.Marshal(map[string]string{"type": "speech.audio.delta", "audio": base64.StdEncoding.EncodeToString(audioData)})
		if marshalErr != nil {
			return nil, types.NewOpenAIError(marshalErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		audioData = []byte("data: " + string(delta) + "\n\ndata: {\"type\":\"speech.audio.done\"}\n\n")
		contentType = "text/event-stream"
	}
	openai.OpenaiTTSHandler(c, &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(audioData))}, info)
	observed := &dto.Usage{UsageSource: "estimated"}
	if info.HasValidOutput() {
		observed.PromptTokens = info.GetEstimatePromptTokens()
		observed.TotalTokens = observed.PromptTokens
	}
	return observed, helper.StreamError(info)
}

func generateRequestID() string {
	return uuid.New().String()
}

type ttsWebSocketBody struct {
	conn *websocket.Conn
	stop func() bool
}

func newTTSWebSocketBody(ctx context.Context, conn *websocket.Conn) *ttsWebSocketBody {
	return &ttsWebSocketBody{conn: conn, stop: context.AfterFunc(ctx, func() { _ = conn.Close() })}
}

func (body *ttsWebSocketBody) Read([]byte) (int, error) {
	return 0, errors.New("VolcEngine audio requires its WebSocket response handler")
}

func (body *ttsWebSocketBody) Close() error {
	body.stop()
	return body.conn.Close()
}

func handleTTSWebSocketResponse(c *gin.Context, conn *websocket.Conn, info *relaycommon.RelayInfo, encoding string) (usage any, apiErr *types.NewAPIError) {
	info.StreamStatus = relaycommon.NewStreamStatus()
	observed := &dto.Usage{UsageSource: "estimated"}
	defer func() {
		if info.HasValidOutput() {
			observed.PromptTokens = info.GetEstimatePromptTokens()
			observed.TotalTokens = observed.PromptTokens
		}
		usage = observed
		apiErr = helper.StreamError(info)
	}()
	conn.SetReadLimit(16 << 20)
	preludeEvents, preludeBytes := 0, 0
	timeout := time.Duration(channelconstant.StreamingTimeout) * time.Second
	if timeout <= 0 {
		timeout = 300 * time.Second
	}
	for {
		if contextErr := c.Request.Context().Err(); contextErr != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, contextErr)
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		msg, recvErr := ReceiveMessage(conn)
		if recvErr != nil {
			reason := relaycommon.StreamEndReasonScannerErr
			var networkErr net.Error
			if errors.As(recvErr, &networkErr) && networkErr.Timeout() {
				reason = relaycommon.StreamEndReasonTimeout
			}
			if contextErr := c.Request.Context().Err(); contextErr != nil {
				reason, recvErr = relaycommon.StreamEndReasonClientGone, contextErr
			}
			info.StreamStatus.SetEndReason(reason, recvErr)
			return
		}
		if msg.Compression != CompressionNone {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("unsupported VolcEngine response compression"))
			return
		}
		if !info.HasValidOutput() && (msg.MsgType != MsgTypeAudioOnlyServer || len(msg.Payload) == 0) {
			preludeEvents++
			preludeBytes += len(msg.Payload)
			if preludeEvents > 64 || preludeBytes > 256<<10 {
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("VolcEngine speech prelude exceeded limit"))
				return
			}
		}
		switch msg.MsgType {
		case MsgTypeError:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("VolcEngine speech failed with code %d", msg.ErrorCode))
			return
		case MsgTypeFrontEndResultServer:
			continue
		case MsgTypeAudioOnlyServer:
			info.UpstreamAccepted = true
			if len(msg.Payload) > 0 {
				if info.IsStream {
					helper.SetEventStreamHeaders(c)
				} else {
					c.Header("Content-Type", getContentTypeByEncoding(encoding))
				}
				if writer, ok := c.Writer.(*helper.StreamPreludeWriter); ok {
					if err := writer.Commit(); err != nil {
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
						return
					}
				}
				var delivered bool
				var writeErr error
				if info.IsStream {
					data, err := common.Marshal(map[string]string{"type": "speech.audio.delta", "audio": base64.StdEncoding.EncodeToString(msg.Payload)})
					if err != nil {
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
						return
					}
					delivered, writeErr = helper.WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: "speech.audio.delta"}, string(data))
				} else {
					var n int
					n, writeErr = c.Writer.Write(msg.Payload)
					delivered = n > 0
					if writeErr == nil && n != len(msg.Payload) {
						writeErr = io.ErrShortWrite
					}
					if writeErr == nil {
						writeErr = helper.FlushWriter(c)
					}
				}
				if delivered {
					info.MarkValidOutput()
					info.SetFirstResponseTime()
				}
				if writeErr != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, writeErr)
					return
				}
			}
			if msg.Sequence < 0 {
				if !info.HasValidOutput() {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("VolcEngine completed without audio"))
					return
				}
				if info.IsStream {
					if _, err := helper.WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: "speech.audio.done"}, `{"type":"speech.audio.done"}`); err != nil {
						info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
						return
					}
				}
				info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
				return
			}
		default:
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, errors.New("unexpected VolcEngine speech message"))
			return
		}
	}
}
