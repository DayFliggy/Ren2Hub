package openai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// The legacy TTS adapter interface returns usage only. StreamStatus also carries
// binary response failures so AudioHelper can return them to the logical request.
func OpenaiTTSHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) *dto.Usage {
	info.StreamStatus = relaycommon.NewStreamStatus()
	usage := &dto.Usage{UsageSource: "estimated"}
	if resp == nil || resp.Body == nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("invalid audio response"))
		return usage
	}
	if info.IsStream {
		return openaiAudioStream(c, resp, info, true)
	}
	defer service.CloseResponseBodyGracefully(resp)
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "json") || strings.Contains(contentType, "text/") {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("expected binary audio response"))
		return usage
	}
	// Withhold binary output until the HTTP body is complete. A short body
	// before client delivery must fail and refund rather than charge for audio.
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		setAudioReadFailure(c, info, err)
		return usage
	}
	if len(body) == 0 {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("empty audio response"))
		return usage
	}
	if err := c.Request.Context().Err(); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return usage
	}
	copyAudioHeaders(c, resp)
	n, err := c.Writer.Write(body)
	if n > 0 {
		info.MarkValidOutput()
		info.SetFirstResponseTime()
		usage = estimateAudioSpeechUsage(c, info, body[:n], int64(n))
	}
	if err != nil || n != len(body) {
		if err == nil {
			err = io.ErrShortWrite
		}
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return usage
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	return usage
}

func OpenaiSTTHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, responseFormat string) (*types.NewAPIError, *dto.Usage) {
	if resp == nil || resp.Body == nil {
		return types.NewOpenAIError(fmt.Errorf("invalid transcription response"), types.ErrorCodeBadResponse, http.StatusBadGateway), nil
	}
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream") {
		info.IsStream = true
		usage := openaiAudioStream(c, resp, info, false)
		return helper.StreamError(info), usage
	}
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway), nil
	}
	usage := &dto.Usage{PromptTokens: info.GetEstimatePromptTokens(), TotalTokens: info.GetEstimatePromptTokens(), UsageSource: "estimated"}
	if responseFormat == "" || responseFormat == "json" || responseFormat == "verbose_json" || responseFormat == "diarized_json" {
		var result struct {
			Text  *string         `json:"text"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := common.Unmarshal(body, &result); err != nil || result.Text == nil {
			return types.NewOpenAIError(fmt.Errorf("invalid transcription response text"), types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
		}
		if len(result.Usage) > 0 && string(result.Usage) != "null" {
			observed, err := decodeAudioUsage(result.Usage, false)
			if err != nil {
				return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
			}
			if observed != nil {
				usage = observed
			}
		}
	} else if len(bytes.TrimSpace(body)) == 0 || strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
		return types.NewOpenAIError(fmt.Errorf("invalid transcription text response"), types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
	}
	if err := c.Request.Context().Err(); err != nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return helper.StreamError(info), nil
	}
	copyAudioHeaders(c, resp)
	n, err := c.Writer.Write(body)
	if n == len(body) {
		info.MarkValidOutput()
		info.SetFirstResponseTime()
	}
	if err != nil || n != len(body) {
		if err == nil {
			err = io.ErrShortWrite
		}
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return helper.StreamError(info), usage
	}
	return nil, usage
}

func openaiAudioStream(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, speech bool) *dto.Usage {
	usage := &dto.Usage{UsageSource: "estimated"}
	var audioBytes int64
	var text, segments strings.Builder
	var completedText *string
	completed := false
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var event struct {
			Type  string          `json:"type"`
			Audio string          `json:"audio"`
			Delta string          `json:"delta"`
			Text  *string         `json:"text"`
			Usage json.RawMessage `json:"usage"`
		}
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			sr.Stop(fmt.Errorf("invalid audio stream event: %w", err))
			return
		}
		terminal, semantic := false, false
		var decoded []byte
		if speech {
			switch event.Type {
			case "speech.audio.delta":
				var err error
				decoded, err = base64.StdEncoding.DecodeString(event.Audio)
				if err != nil || len(decoded) == 0 {
					sr.Stop(fmt.Errorf("invalid speech audio delta"))
					return
				}
				semantic = true
			case "speech.audio.done":
				terminal = true
			default:
				sr.Stop(fmt.Errorf("unexpected speech event %q", event.Type))
				return
			}
		} else {
			switch event.Type {
			case "transcript.text.delta":
				semantic = event.Delta != ""
			case "transcript.text.segment":
				semantic = event.Text != nil && *event.Text != ""
			case "transcript.text.done":
				if event.Text == nil {
					sr.Stop(fmt.Errorf("transcription terminal is missing text"))
					return
				}
				semantic, terminal = true, true
			default:
				sr.Stop(fmt.Errorf("unexpected transcription event %q", event.Type))
				return
			}
		}
		var observed *dto.Usage
		if len(event.Usage) > 0 && string(event.Usage) != "null" {
			var err error
			observed, err = decodeAudioUsage(event.Usage, speech)
			if err != nil {
				sr.Stop(err)
				return
			}
		}
		if speech && terminal && audioBytes == 0 {
			sr.Stop(fmt.Errorf("speech completed without audio"))
			return
		}
		if observed != nil {
			usage = observed
		}
		if semantic {
			// Audio deltas may exceed the prelude byte limit; commit only
			// after validation, then account for bytes actually delivered.
			if writer, ok := c.Writer.(*helper.StreamPreludeWriter); ok {
				if err := writer.Commit(); err != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					sr.Stop(nil)
					return
				}
			}
		}
		delivered, err := helper.WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: event.Type}, data)
		if semantic && delivered {
			sr.MarkValidOutput()
			info.MarkValidOutput()
			info.SetFirstResponseTime()
			audioBytes += int64(len(decoded))
			if event.Type == "transcript.text.delta" {
				text.WriteString(event.Delta)
			} else if event.Type == "transcript.text.segment" && event.Text != nil {
				segments.WriteString(*event.Text)
				segments.WriteByte('\n')
			} else if event.Type == "transcript.text.done" {
				completedText = event.Text
			}
		}
		if err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
			sr.Stop(nil)
			return
		}
		if terminal {
			completed = true
			sr.Done()
		}
	})
	if !completed && info.StreamStatus.IsNormalEnd() {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonEOF, fmt.Errorf("audio stream ended without protocol completion"))
	}
	if usage.UsageSource != "provider" && info.HasValidOutput() {
		if speech {
			usage = estimateAudioSpeechUsage(c, info, nil, audioBytes)
		} else {
			deliveredText := text.String()
			if completedText != nil {
				deliveredText = *completedText
			} else if deliveredText == "" {
				deliveredText = segments.String()
			}
			usage = service.ResponseText2Usage(c, deliveredText, info.UpstreamModelName, info.GetEstimatePromptTokens())
			usage.UsageSource = "estimated"
			usage.PromptTokensDetails.AudioTokens = usage.PromptTokens
			usage.CompletionTokenDetails.TextTokens = usage.CompletionTokens
		}
	}
	return usage
}

func copyAudioHeaders(c *gin.Context, resp *http.Response) {
	for k, values := range resp.Header {
		if len(values) > 0 && service.ShouldCopyUpstreamHeader(c, k, values) {
			c.Writer.Header()[k] = values
		}
	}
	c.Status(resp.StatusCode)
}

func setAudioReadFailure(c *gin.Context, info *relaycommon.RelayInfo, err error) {
	if contextErr := c.Request.Context().Err(); contextErr != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, contextErr)
	} else {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonScannerErr, err)
	}
}

func estimateAudioSpeechUsage(c *gin.Context, info *relaycommon.RelayInfo, body []byte, size int64) *dto.Usage {
	common.SetContextKey(c, constant.ContextKeyLocalCountTokens, true)
	usage := &dto.Usage{PromptTokens: info.GetEstimatePromptTokens(), UsageSource: "estimated"}
	format := "mp3"
	if request, ok := info.Request.(*dto.AudioRequest); ok && request.ResponseFormat != "" {
		format = request.ResponseFormat
	}
	var duration float64
	if format == "pcm" {
		duration = float64(size) / (24000 * 2)
	} else if len(body) > 0 {
		var err error
		duration, err = common.GetAudioDuration(c.Request.Context(), bytes.NewReader(body), "."+format)
		if err != nil {
			logger.LogWarn(c, fmt.Sprintf("failed to get audio duration; estimating from delivered bytes: %v", err))
		}
	}
	if duration > 0 {
		usage.CompletionTokens = common.QuotaRound(math.Ceil(duration) / 60 * 1000)
	} else {
		usage.CompletionTokens = common.QuotaRound(math.Ceil(float64(size) / 1000))
	}
	usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	usage.CompletionTokenDetails.AudioTokens = usage.CompletionTokens
	usage.TotalTokens = common.QuotaFromFloat(float64(usage.PromptTokens) + float64(usage.CompletionTokens))
	return usage
}

func decodeAudioUsage(raw json.RawMessage, speech bool) (*dto.Usage, error) {
	var wire struct {
		dto.Usage
		Type              string                 `json:"type"`
		Seconds           *json.Number           `json:"seconds"`
		InputTokenDetails *dto.InputTokenDetails `json:"input_token_details"`
	}
	if err := common.Unmarshal(raw, &wire); err != nil {
		return nil, fmt.Errorf("invalid audio usage: %w", err)
	}
	usage := &wire.Usage
	counts := []int{usage.PromptTokens, usage.CompletionTokens, usage.InputTokens, usage.OutputTokens, usage.TotalTokens,
		usage.CompletionTokenDetails.TextTokens, usage.CompletionTokenDetails.AudioTokens, usage.CompletionTokenDetails.ImageTokens, usage.CompletionTokenDetails.ReasoningTokens}
	for _, details := range []*dto.InputTokenDetails{&usage.PromptTokensDetails, usage.InputTokensDetails, wire.InputTokenDetails} {
		if details != nil {
			counts = append(counts, details.TextTokens, details.AudioTokens, details.CachedTokens, details.ImageTokens, details.CacheWriteTokens, details.CachedCreationTokens)
		}
	}
	for _, count := range counts {
		if count < 0 || count > math.MaxInt32 {
			return nil, fmt.Errorf("invalid audio usage token count")
		}
	}
	if wire.Type == "duration" {
		if wire.Seconds == nil {
			return nil, fmt.Errorf("audio duration is missing")
		}
		seconds, err := billing.ParseAmountUSD(wire.Seconds.String())
		if err != nil {
			return nil, fmt.Errorf("invalid audio usage duration")
		}
		return &dto.Usage{UsageSource: "provider_duration", MeasuredQuantities: map[string]string{"seconds": seconds.String()}}, nil
	}
	if wire.Type != "" && wire.Type != "tokens" {
		return nil, fmt.Errorf("unsupported audio usage type %q", wire.Type)
	}
	if !dto.HasJSONTokenCounts(raw, "input_tokens", "output_tokens") && !dto.HasJSONTokenCounts(raw, "prompt_tokens", "completion_tokens") {
		return nil, fmt.Errorf("audio usage requires explicit input and output counts")
	}
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	total := int64(usage.PromptTokens) + int64(usage.CompletionTokens)
	if total > math.MaxInt32 || usage.TotalTokens != 0 && int64(usage.TotalTokens) != total {
		return nil, fmt.Errorf("inconsistent audio usage total")
	}
	usage.TotalTokens = int(total)
	if details := wire.InputTokenDetails; details != nil {
		usage.PromptTokensDetails = *details
	} else if details := usage.InputTokensDetails; details != nil {
		usage.PromptTokensDetails = *details
	}
	inputDetails := &usage.PromptTokensDetails
	outputDetails := &usage.CompletionTokenDetails
	if int64(inputDetails.AudioTokens)+int64(inputDetails.TextTokens) > int64(usage.PromptTokens) || int64(outputDetails.AudioTokens)+int64(outputDetails.TextTokens) > int64(usage.CompletionTokens) {
		return nil, fmt.Errorf("inconsistent audio usage details")
	}
	if speech {
		inputDetails.TextTokens = usage.PromptTokens - inputDetails.AudioTokens
		outputDetails.AudioTokens = usage.CompletionTokens - outputDetails.TextTokens
	} else {
		inputDetails.AudioTokens = usage.PromptTokens - inputDetails.TextTokens
		outputDetails.TextTokens = usage.CompletionTokens - outputDetails.AudioTokens
	}
	usage.BillingUsage = nil
	usage.UsageSource = "provider"
	return usage, nil
}
