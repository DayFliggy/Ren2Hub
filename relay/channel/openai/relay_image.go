package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// DeliverConvertedImage applies the same validation, delivery and image-count
// accounting to provider-specific responses after they become OpenAI images.
// Adaptors providing estimates must set UsageSource explicitly.
func DeliverConvertedImage(c *gin.Context, info *relaycommon.RelayInfo, source *http.Response, response *dto.ImageResponse, estimate *dto.Usage) (*dto.Usage, *types.NewAPIError) {
	body, err := common.Marshal(struct {
		*dto.ImageResponse
		Usage *dto.Usage `json:"usage,omitempty"`
	}{response, estimate})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	converted := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}
	if source != nil && source.Header != nil {
		converted.Header = source.Header.Clone()
	}
	converted.Header.Set("Content-Type", "application/json")
	var usage *dto.Usage
	var apiErr *types.NewAPIError
	if info.IsStream {
		usage, apiErr = OpenaiImageStreamHandler(c, info, converted)
	} else {
		usage, apiErr = OpenaiImageHandler(c, info, converted)
	}
	if usage != nil {
		usage.UsageSource = "estimated"
		if estimate != nil {
			usage.UsageSource = "provider"
			if estimate.UsageSource != "" {
				usage.UsageSource = estimate.UsageSource
			}
		}
	}
	return usage, apiErr
}

func updateOpenAIImageCount(info *relaycommon.RelayInfo, count int64) {
	if info == nil || !info.PriceData.UsePrice || count <= 0 || count > int64(dto.MaxImageN) {
		return
	}
	info.PriceData.AddOtherRatio("n", float64(count))
}

// OpenaiImageHandler handles non-streaming OpenAI image responses
// (generations/edits), returning the parsed usage for billing.
func OpenaiImageHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid image response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}

	var usageResp dto.SimpleResponse
	err = common.Unmarshal(responseBody, &usageResp)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		status := resp.StatusCode
		if status < 400 {
			status = http.StatusBadGateway
		}
		return nil, types.WithOpenAIError(*oaiError, status)
	}

	imageCount, err := validateOpenAIImages(responseBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	if err := normalizeOpenAIUsage(&usageResp.Usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	updateOpenAIImageCount(info, imageCount)

	if _, err := service.CopyResponseBytes(c, resp, responseBody); err != nil {
		info.StreamStatus = relaycommon.NewStreamStatus()
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return &usageResp.Usage, helper.StreamError(info)
	}
	info.MarkValidOutput()
	return &usageResp.Usage, nil
}

// normalizeOpenAIUsage maps the OpenAI Images usage shape (input_tokens /
// output_tokens / input_tokens_details) onto the canonical prompt/completion
// fields. It is used only on the OpenAI image relay paths (generations/edits,
// streaming and non-streaming): the image API never returns prompt_tokens /
// completion_tokens, so the overwrite (=) semantics here are equivalent to the
// previous additive (+=) behavior while avoiding any future double-counting if
// both field sets are ever populated. Do not reuse this on chat/embedding paths
// without revisiting the overwrite semantics.
func normalizeOpenAIUsage(usage *dto.Usage) error {
	if usage == nil {
		return nil
	}
	// Reject untrusted arithmetic before combining provider token counts.
	counts := []int{usage.InputTokens, usage.OutputTokens, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens,
		usage.PromptTokensDetails.TextTokens, usage.PromptTokensDetails.ImageTokens, usage.PromptTokensDetails.AudioTokens,
		usage.PromptTokensDetails.CachedTokens, usage.PromptTokensDetails.CachedCreationTokens, usage.PromptTokensDetails.CacheWriteTokens,
		usage.CompletionTokenDetails.TextTokens, usage.CompletionTokenDetails.ImageTokens, usage.CompletionTokenDetails.AudioTokens, usage.CompletionTokenDetails.ReasoningTokens}
	if d := usage.InputTokensDetails; d != nil {
		counts = append(counts, d.TextTokens, d.ImageTokens, d.AudioTokens, d.CachedTokens, d.CachedCreationTokens, d.CacheWriteTokens)
	}
	for _, count := range counts {
		if count < 0 || count > math.MaxInt32 {
			return fmt.Errorf("invalid image usage token count")
		}
	}
	usage.BillingUsage = nil
	usage.UsageSource = "provider"
	if usage.InputTokens != 0 {
		usage.PromptTokens = usage.InputTokens
	}
	if usage.OutputTokens != 0 {
		usage.CompletionTokens = usage.OutputTokens
	}
	if usage.InputTokensDetails != nil {
		usage.PromptTokensDetails.CachedTokens = usage.InputTokensDetails.CachedTokens
		usage.PromptTokensDetails.CachedCreationTokens = usage.InputTokensDetails.CachedCreationTokens
		usage.PromptTokensDetails.CacheWriteTokens = usage.InputTokensDetails.CacheWriteTokens
		usage.PromptTokensDetails.ImageTokens = usage.InputTokensDetails.ImageTokens
		usage.PromptTokensDetails.TextTokens = usage.InputTokensDetails.TextTokens
		usage.PromptTokensDetails.AudioTokens = usage.InputTokensDetails.AudioTokens
	}
	total := int64(usage.PromptTokens) + int64(usage.CompletionTokens)
	if total > math.MaxInt32 || usage.TotalTokens != 0 && int64(usage.TotalTokens) < total {
		return fmt.Errorf("inconsistent image usage total")
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = int(total)
	}
	details := usage.PromptTokensDetails
	if int64(details.TextTokens)+int64(details.ImageTokens)+int64(details.AudioTokens) > int64(usage.PromptTokens) || details.CachedTokens > usage.PromptTokens {
		return fmt.Errorf("inconsistent image input usage details")
	}
	if usage.TotalTokens == 0 {
		usage.UsageSource = "estimated"
	}
	return nil
}

func validateOpenAIImages(body []byte) (int64, error) {
	items := gjson.GetBytes(body, "data")
	if !items.IsArray() || len(items.Array()) == 0 || len(items.Array()) > dto.MaxImageN {
		return 0, fmt.Errorf("invalid image response data")
	}
	for _, item := range items.Array() {
		if !hasOpenAIImagePayload(item) {
			return 0, fmt.Errorf("image response has no image payload")
		}
	}
	return int64(len(items.Array())), nil
}

func hasOpenAIImagePayload(value gjson.Result) bool {
	for _, field := range []string{"b64_json", "url"} {
		image := value.Get(field)
		if image.Type == gjson.String && strings.TrimSpace(image.Str) != "" {
			return true
		}
	}
	return false
}

func OpenaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid image stream response")
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return OpenaiImageHandler(c, info, resp)
	}
	if !strings.Contains(contentType, "text/event-stream") {
		return openaiImageJSONAsStreamHandler(c, info, resp)
	}
	usage := &dto.Usage{UsageSource: "estimated"}
	var lastStreamData []byte
	var completedImages int64
	var partialOutput bool

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		raw := common.StringToByteSlice(data)
		var chunk struct {
			Type  string     `json:"type"`
			Usage *dto.Usage `json:"usage"`
		}
		if err := common.Unmarshal(raw, &chunk); err != nil {
			sr.Stop(fmt.Errorf("invalid image stream event: %w", err))
			return
		}
		if isOpenAIImageStreamErrorEvent(raw) {
			sr.Stop(fmt.Errorf("%s", extractOpenAIImageStreamErrorMessage(raw)))
			return
		}
		completed := chunk.Type == "image_generation.completed" || chunk.Type == "image_edit.completed"
		partial := chunk.Type == "image_generation.partial_image" || chunk.Type == "image_edit.partial_image"
		if (completed || partial) && !hasOpenAIImagePayload(gjson.ParseBytes(raw)) {
			sr.Stop(fmt.Errorf("image stream event has no image payload"))
			return
		}
		if chunk.Usage != nil {
			if err := normalizeOpenAIUsage(chunk.Usage); err != nil {
				sr.Stop(err)
				return
			}
			usage = chunk.Usage
		}
		if completed && completedImages >= int64(dto.MaxImageN) {
			sr.Stop(fmt.Errorf("image stream exceeds maximum image count"))
			return
		}
		if completed || partial {
			// A validated image is semantic output, often larger than the
			// prelude limit. Commit preceding events before writing its bytes.
			if prelude, ok := c.Writer.(*helper.StreamPreludeWriter); ok {
				if err := prelude.Commit(); err != nil {
					info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
					return
				}
			}
		}
		delivered, writeErr := writeOpenaiImageStreamChunk(c, raw)
		if (completed || partial) && delivered {
			info.MarkValidOutput()
			info.SetFirstResponseTime()
			if completed {
				completedImages++
			}
			partialOutput = partialOutput || partial
		}
		if writeErr != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, writeErr)
			sr.Stop(writeErr)
			return
		}
		lastStreamData = raw
	})
	if info.StreamStatus != nil && info.StreamStatus.IsNormalEnd() && completedImages == 0 {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, fmt.Errorf("image stream ended without a completed image"))
	}
	applyUsagePostProcessing(info, usage, lastStreamData)
	if err := normalizeOpenAIUsage(usage); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, err)
	}
	// Bill delivered images, never the requested maximum after interruption.
	// Partial output with no final image incurs one fixed-price operation.
	count := completedImages
	if count == 0 && partialOutput {
		count = 1
	}
	updateOpenAIImageCount(info, count)
	if info.StreamStatus != nil && info.StreamStatus.IsNormalEnd() {
		if err := writeOpenaiImageStreamDone(c); err != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		}
	}
	return usage, helper.StreamError(info)
}

// writeOpenaiImageStreamChunk rebuilds the SSE frame for an image stream chunk:
// it emits an "event:" line derived from the JSON "type" field (when present)
// followed by the verbatim "data:" payload, mirroring helper.ResponseChunkData.
func writeOpenaiImageStreamChunk(c *gin.Context, data []byte) (bool, error) {
	var payload struct {
		Type string `json:"type"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false, err
	}
	return helper.WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: payload.Type}, string(data))
}

// isOpenAIImageStreamErrorEvent detects upstream error chunks by JSON content
// only ("type" of error/upstream_error, or a non-empty "error" field). The SSE
// "event:" line is not available here: StreamScannerHandler delivers only the
// "data:" payload. A payload carrying just a "message" key is deliberately NOT
// treated as an error to avoid false positives.
func isOpenAIImageStreamErrorEvent(data []byte) bool {
	if !json.Valid(data) {
		return false
	}
	var payload struct {
		Type  string          `json:"type"`
		Error json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return false
	}
	payloadType := strings.ToLower(strings.TrimSpace(payload.Type))
	return payloadType == "error" || payloadType == "upstream_error" || len(payload.Error) > 0
}

func extractOpenAIImageStreamErrorMessage(data []byte) string {
	if len(data) == 0 || !json.Valid(data) {
		return "upstream image stream returned error event"
	}
	var payload struct {
		Message string          `json:"message"`
		Error   json.RawMessage `json:"error"`
	}
	if err := common.Unmarshal(data, &payload); err != nil {
		return "upstream image stream returned error event"
	}
	if msg := strings.TrimSpace(payload.Message); msg != "" {
		return msg
	}
	if len(payload.Error) > 0 {
		var nested struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(payload.Error, &nested); err == nil {
			if msg := strings.TrimSpace(nested.Message); msg != "" {
				return msg
			}
		}
		if msg := strings.TrimSpace(common.JsonRawMessageToString(payload.Error)); msg != "" {
			return msg
		}
	}
	return "upstream image stream returned error event"
}

func openaiImageJSONAsStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway)
	}

	// Only decode usage/error. Do not Unmarshal data[] into dto.ImageResponse —
	// b64_json values are large and would be copied into Go strings then
	// re-marshaled for each SSE event.
	var usageResp dto.SimpleResponse
	if err := common.Unmarshal(responseBody, &usageResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if oaiError := usageResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		status := resp.StatusCode
		if status < 400 {
			status = http.StatusBadGateway
		}
		return nil, types.WithOpenAIError(*oaiError, status)
	}
	applyUsagePostProcessing(info, &usageResp.Usage, responseBody)
	if err := normalizeOpenAIUsage(&usageResp.Usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	imageCount, err := validateOpenAIImages(responseBody)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	info.StreamStatus = relaycommon.NewStreamStatus()

	helper.SetEventStreamHeaders(c)
	c.Status(http.StatusOK)

	created := gjson.GetBytes(responseBody, "created").Int()
	if created == 0 {
		created = time.Now().Unix()
	}
	validUsage := service.ValidUsage(&usageResp.Usage)
	var usageJSON []byte
	if validUsage {
		usageJSON, err = common.Marshal(usageResp.Usage)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
	}

	for i := int64(0); i < imageCount; i++ {
		image := gjson.GetBytes(responseBody, "data."+strconv.FormatInt(i, 10))
		payload := []byte(`{"type":"image_generation.completed"}`)
		payload, err = sjson.SetBytes(payload, "created_at", created)
		if err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if validUsage {
			payload, err = sjson.SetRawBytes(payload, "usage", usageJSON)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		// b64_json goes last: every sjson.Set* reallocates the whole payload,
		// so inserting the large blob after all small fields avoids re-copying
		// multi-MB buffers.
		for _, field := range []string{"url", "revised_prompt", "b64_json"} {
			value := image.Get(field)
			if value.Type != gjson.String || value.Raw == `""` {
				continue
			}
			raw := []byte(value.Raw)
			if value.Index > 0 {
				raw = responseBody[value.Index : value.Index+len(value.Raw)]
			}
			payload, err = sjson.SetRawBytes(payload, field, raw)
			if err != nil {
				return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
			}
		}
		delivered, writeErr := helper.WriteResponseChunkData(c, dto.ResponsesStreamResponse{Type: "image_generation.completed"}, string(payload))
		if delivered {
			info.SetFirstResponseTime()
			info.MarkValidOutput()
			info.ReceivedResponseCount++
			updateOpenAIImageCount(info, i+1)
		}
		if writeErr != nil {
			info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, writeErr)
			return &usageResp.Usage, helper.StreamError(info)
		}
	}
	if err := writeOpenaiImageStreamDone(c); err != nil {
		info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, err)
		return &usageResp.Usage, helper.StreamError(info)
	}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonDone, nil)
	return &usageResp.Usage, nil
}

func writeOpenaiImageStreamDone(c *gin.Context) error {
	return helper.StringData(c, "[DONE]")
}
