package gemini

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// Google can carry an error inside HTTP 200, including an SSE data frame.
// Validate before writing so protocol conversion cannot turn it into success.
func parseGeminiGenerationResponse(c *gin.Context, data []byte, stream, priorOutput bool) (*dto.GeminiChatResponse, *types.NewAPIError) {
	var envelope struct {
		Error        json.RawMessage `json:"error"`
		ModelVersion string          `json:"modelVersion"`
		ResponseID   string          `json:"responseId"`
	}
	if err := common.Unmarshal(data, &envelope); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if len(envelope.Error) > 0 && !bytes.Equal(bytes.TrimSpace(envelope.Error), []byte("null")) {
		apiErr := geminiEnvelopeError(envelope.Error)
		if !priorOutput && apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 {
			c.Set("billing_upstream_rejected", true)
		}
		return nil, apiErr
	}
	var response dto.GeminiChatResponse
	if err := common.Unmarshal(data, &response); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if feedback := response.PromptFeedback; feedback != nil && feedback.BlockReason != nil {
		reason := strings.TrimSpace(*feedback.BlockReason)
		if reason != "" && reason != "BLOCK_REASON_UNSPECIFIED" {
			if !priorOutput && !geminiResponseHasOutput(&response) && !dto.HasGeminiUsageMetadataTokens(response.GetUsageMetadata()) {
				c.Set("billing_upstream_rejected", true)
			}
			return &response, geminiPolicyError(c, reason)
		}
	}
	hasOutput := priorOutput || geminiResponseHasOutput(&response)
	for _, candidate := range response.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason != "" && *candidate.FinishReason != "FINISH_REASON_UNSPECIFIED" && !geminiKnownFinishReason(*candidate.FinishReason) {
			return &response, types.NewOpenAIError(fmt.Errorf("unknown Gemini finish reason: %s", *candidate.FinishReason), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		}
		if candidate.FinishReason != nil && geminiPolicyFinishReason(*candidate.FinishReason) && !hasOutput {
			if !dto.HasGeminiUsageMetadataTokens(response.GetUsageMetadata()) {
				c.Set("billing_upstream_rejected", true)
			}
			return &response, geminiPolicyError(c, *candidate.FinishReason)
		}
	}
	if len(response.Candidates) == 0 {
		// Usage and model metadata may arrive separately from stream content.
		if stream && (response.HasUsageMetadata || envelope.ModelVersion != "" || envelope.ResponseID != "") {
			return &response, nil
		}
		return &response, geminiEmptyResponseError()
	}
	if !stream && !hasOutput {
		for _, candidate := range response.Candidates {
			if candidate.FinishReason != nil && geminiKnownFinishReason(*candidate.FinishReason) {
				// For example MAX_TOKENS can exhaust a reasoning budget before text.
				return &response, nil
			}
		}
		return &response, geminiEmptyResponseError()
	}
	return &response, nil
}

func geminiEnvelopeError(data []byte) *types.NewAPIError {
	var upstream struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	if err := common.Unmarshal(data, &upstream); err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	status := upstream.Code
	if status < 400 || status > 599 {
		switch upstream.Status {
		case "INVALID_ARGUMENT", "FAILED_PRECONDITION", "OUT_OF_RANGE":
			status = http.StatusBadRequest
		case "UNAUTHENTICATED":
			status = http.StatusUnauthorized
		case "PERMISSION_DENIED":
			status = http.StatusForbidden
		case "NOT_FOUND":
			status = http.StatusNotFound
		case "RESOURCE_EXHAUSTED":
			status = http.StatusTooManyRequests
		case "DEADLINE_EXCEEDED":
			status = http.StatusGatewayTimeout
		case "UNAVAILABLE":
			status = http.StatusServiceUnavailable
		case "ABORTED":
			status = http.StatusConflict
		default:
			status = http.StatusBadGateway
		}
	}
	code := upstream.Status
	if code == "" {
		code = "gemini_upstream_error"
	}
	if upstream.Message == "" {
		upstream.Message = "Gemini API returned an error: " + code
	}
	return types.WithOpenAIError(types.OpenAIError{Message: upstream.Message, Type: "upstream_error", Code: code}, status)
}

func geminiPolicyFinishReason(reason string) bool {
	switch reason {
	case "SAFETY", "RECITATION", "LANGUAGE", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "IMAGE_SAFETY", "IMAGE_PROHIBITED_CONTENT", "IMAGE_RECITATION":
		return true
	default:
		return false
	}
}

func geminiKnownFinishReason(reason string) bool {
	if geminiPolicyFinishReason(reason) {
		return true
	}
	switch reason {
	case "STOP", "MAX_TOKENS", "OTHER", "MALFORMED_FUNCTION_CALL", "UNEXPECTED_TOOL_CALL", "TOO_MANY_TOOL_CALLS", "IMAGE_OTHER", "NO_IMAGE":
		return true
	default:
		return false
	}
}

func geminiPolicyError(c *gin.Context, reason string) *types.NewAPIError {
	common.SetContextKey(c, constant.ContextKeyAdminRejectReason, "gemini_block_reason="+reason)
	return types.NewOpenAIError(fmt.Errorf("request blocked by Gemini API: %s", reason), types.ErrorCodePromptBlocked, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
}

func geminiEmptyResponseError() *types.NewAPIError {
	return types.NewOpenAIError(errors.New("empty response from Gemini API"), types.ErrorCodeEmptyResponse, http.StatusBadGateway)
}

func geminiResponseHasOutput(response *dto.GeminiChatResponse) bool {
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" || (part.FunctionCall != nil && part.FunctionCall.FunctionName != "") || (part.InlineData != nil && part.InlineData.Data != "") || (part.FileData != nil && part.FileData.FileUri != "") || (part.ExecutableCode != nil && part.ExecutableCode.Code != "") || (part.CodeExecutionResult != nil && part.CodeExecutionResult.Output != "") {
				return true
			}
		}
	}
	return false
}

func readGeminiGenerationResponse(c *gin.Context, resp *http.Response) ([]byte, *dto.GeminiChatResponse, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	response, apiErr := parseGeminiGenerationResponse(c, body, false, false)
	if apiErr != nil {
		apiErr.RetryAfter, _ = service.ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	}
	return body, response, apiErr
}
