package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	taskdto "github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func MidjourneyErrorWrapper(code int, desc string) *taskdto.MidjourneyResponse {
	return &taskdto.MidjourneyResponse{
		Code:        code,
		Description: desc,
	}
}

func MidjourneyErrorWithStatusCodeWrapper(code int, desc string, statusCode int) *taskdto.MidjourneyResponseWithStatusCode {
	return &taskdto.MidjourneyResponseWithStatusCode{
		StatusCode: statusCode,
		Response:   *MidjourneyErrorWrapper(code, desc),
	}
}

//// OpenAIErrorWrapper wraps an error into an OpenAIErrorWithStatusCode
//func OpenAIErrorWrapper(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	text := err.Error()
//	lowerText := strings.ToLower(text)
//	if !strings.HasPrefix(lowerText, "get file base64 from url") && !strings.HasPrefix(lowerText, "mime type is not supported") {
//		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
//			common.SysLog(fmt.Sprintf("error: %s", text))
//			text = "请求上游地址失败"
//		}
//	}
//	openAIError := dto.OpenAIError{
//		Message: text,
//		Type:    "new_api_error",
//		Code:    code,
//	}
//	return &dto.OpenAIErrorWithStatusCode{
//		Error:      openAIError,
//		StatusCode: statusCode,
//	}
//}
//
//func OpenAIErrorWrapperLocal(err error, code string, statusCode int) *dto.OpenAIErrorWithStatusCode {
//	openaiErr := OpenAIErrorWrapper(err, code, statusCode)
//	openaiErr.LocalError = true
//	return openaiErr
//}

func ClaudeErrorWrapper(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	text := err.Error()
	lowerText := strings.ToLower(text)
	if !strings.HasPrefix(lowerText, "get file base64 from url") {
		if strings.Contains(lowerText, "post") || strings.Contains(lowerText, "dial") || strings.Contains(lowerText, "http") {
			common.SysLog(common.LogText("error: %s", text))
			text = "request to the upstream address failed"
		}
	}
	claudeError := types.ClaudeError{
		Message: text,
		Type:    "new_api_error",
	}
	return &dto.ClaudeErrorWithStatusCode{
		Error:      claudeError,
		StatusCode: statusCode,
	}
}

func ClaudeErrorWrapperLocal(err error, code string, statusCode int) *dto.ClaudeErrorWithStatusCode {
	claudeErr := ClaudeErrorWrapper(err, code, statusCode)
	claudeErr.LocalError = true
	return claudeErr
}

func RelayErrorHandler(ctx context.Context, resp *http.Response, showBodyWhenFail bool) (newApiErr *types.NewAPIError) {
	var addresses []string
	if c, ok := ctx.(*gin.Context); ok {
		addresses = common.UpstreamAddresses(c)
	}
	defer func() {
		if newApiErr != nil {
			types.ErrOptionWithUpstreamAddresses(addresses...)(newApiErr)
		}
	}()
	newApiErr = types.InitOpenAIError(types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	retryAfter, hasRetryAfter := ParseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	if hasRetryAfter {
		newApiErr.RetryAfter = retryAfter
	}

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	CloseResponseBodyGracefully(resp)
	var errResponse dto.GeneralErrorResponse
	responseBodyText := string(responseBody)
	responseBodyPreview := common.LocalLogPreview(common.MaskSensitiveInfo(responseBodyText, addresses...))
	buildErrWithBody := func(message string) error {
		if message == "" {
			return fmt.Errorf("bad response status code %d, body: %s", resp.StatusCode, responseBodyText)
		}
		return fmt.Errorf("bad response status code %d, message: %s, body: %s", resp.StatusCode, message, responseBodyText)
	}

	err = common.Unmarshal(responseBody, &errResponse)
	if err != nil {
		if showBodyWhenFail {
			newApiErr.Err = buildErrWithBody("")
		} else {
			logger.LogError(ctx, common.LogText("bad response status code %d, body: %s", resp.StatusCode, responseBodyPreview))
			newApiErr.Err = fmt.Errorf("bad response status code %d", resp.StatusCode)
		}
		return
	}

	if common.GetJsonType(errResponse.Error) == "object" {
		// General format error (OpenAI, Anthropic, Gemini, etc.)
		oaiError := errResponse.TryToOpenAIError()
		if oaiError != nil {
			newApiErr = types.WithOpenAIError(*oaiError, resp.StatusCode)
			if hasRetryAfter {
				newApiErr.RetryAfter = retryAfter
			}
			if showBodyWhenFail {
				newApiErr.Err = buildErrWithBody(newApiErr.Error())
			}
			return
		}
	}
	message := errResponse.ToMessage()
	if message == "" {
		// The body parsed as JSON but carried no usable error message; log the
		// raw body so the upstream failure remains diagnosable.
		logger.LogError(ctx, common.LogText("bad response status code %d with empty error message, body: %s", resp.StatusCode, responseBodyPreview))
	}
	newApiErr = types.NewOpenAIError(errors.New(message), types.ErrorCodeBadResponseStatusCode, resp.StatusCode)
	if hasRetryAfter {
		newApiErr.RetryAfter = retryAfter
	}
	if showBodyWhenFail {
		newApiErr.Err = buildErrWithBody(newApiErr.Error())
	}
	return
}

func ResetStatusCode(newApiErr *types.NewAPIError, statusCodeMappingStr string) {
	if newApiErr == nil {
		return
	}
	if statusCodeMappingStr == "" || statusCodeMappingStr == "{}" {
		return
	}
	statusCodeMapping := make(map[string]any)
	err := common.Unmarshal([]byte(statusCodeMappingStr), &statusCodeMapping)
	if err != nil {
		return
	}
	if newApiErr.StatusCode == http.StatusOK {
		return
	}
	codeStr := strconv.Itoa(newApiErr.StatusCode)
	if value, ok := statusCodeMapping[codeStr]; ok {
		intCode, ok := parseStatusCodeMappingValue(value)
		if !ok {
			return
		}
		newApiErr.StatusCode = intCode
	}
}

func parseStatusCodeMappingValue(value any) (int, bool) {
	switch v := value.(type) {
	case string:
		if v == "" {
			return 0, false
		}
		statusCode, err := strconv.Atoi(v)
		if err != nil {
			return 0, false
		}
		return statusCode, true
	case float64:
		if v != math.Trunc(v) {
			return 0, false
		}
		return int(v), true
	case int:
		return v, true
	case json.Number:
		statusCode, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return statusCode, true
	default:
		return 0, false
	}
}

func TaskErrorWrapperLocal(err error, code string, statusCode int, upstreamURLs ...string) *taskdto.TaskError {
	openaiErr := TaskErrorWrapper(err, code, statusCode, upstreamURLs...)
	openaiErr.LocalError = true
	return openaiErr
}

func TaskErrorWrapper(err error, code string, statusCode int, upstreamURLs ...string) *taskdto.TaskError {
	text := common.MaskSensitiveInfo(err.Error(), upstreamURLs...)
	//避免暴露内部错误
	taskError := &taskdto.TaskError{
		Code:       code,
		Message:    text,
		StatusCode: statusCode,
		Error:      err,
	}

	return taskError
}

// TaskErrorFromAPIError preserves admission/permission errors across the task API.
func TaskErrorFromAPIError(apiErr *types.NewAPIError, upstreamURLs ...string) *taskdto.TaskError {
	if apiErr == nil {
		return nil
	}
	return &taskdto.TaskError{
		Code:       string(apiErr.GetErrorCode()),
		Message:    apiErr.MaskSensitiveError(upstreamURLs...),
		StatusCode: apiErr.StatusCode,
		LocalError: true,
		Error:      apiErr,
	}
}

func APIErrorFromTaskError(taskErr *taskdto.TaskError) *types.NewAPIError {
	if taskErr == nil {
		return nil
	}
	var apiErr *types.NewAPIError
	if errors.As(taskErr.Error, &apiErr) {
		return apiErr
	}
	code := types.ErrorCodeBadResponseStatusCode
	if taskErr.LocalError {
		code = types.ErrorCodeInvalidRequest
	} else if taskErr.Code == "do_request_failed" {
		code = types.ErrorCodeDoRequestFailed
	}
	apiErr = types.NewErrorWithStatusCode(errors.New(taskErr.Message), code, taskErr.StatusCode)
	if taskErr.LocalError {
		types.ErrOptionWithSkipRetry()(apiErr)
	}
	return apiErr
}

// ResponsesAPIError preserves provider failure fields across JSON, SSE and WS.
// A symbolic code is not an HTTP status; only numeric 400..599 codes select it.
func ResponsesAPIError(provider *types.OpenAIError, fallback string) *types.NewAPIError {
	if provider == nil {
		return types.NewOpenAIError(errors.New(fallback), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	status := http.StatusBadGateway
	if parsed, err := strconv.Atoi(fmt.Sprint(provider.Code)); err == nil && parsed >= 400 && parsed <= 599 {
		status = parsed
	}
	return types.WithOpenAIError(*provider, status)
}

// MaskResponsesErrorPayload preserves unknown envelope fields while masking
// the error message at either Responses error location.
func MaskResponsesErrorPayload(body []byte, addresses ...string) ([]byte, error) {
	var envelope map[string]common.RawMessage
	if err := common.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	for _, key := range []string{"message", "error"} {
		raw, ok := envelope[key]
		if !ok {
			continue
		}
		var message string
		if common.Unmarshal(raw, &message) == nil {
			masked, err := common.Marshal(common.MaskSensitiveInfo(message, addresses...))
			if err != nil {
				return nil, err
			}
			envelope[key] = masked
		}
	}
	for _, key := range []string{"error", "response"} {
		if raw := envelope[key]; len(raw) > 0 && common.GetJsonType(raw) == "object" {
			masked, err := MaskResponsesErrorPayload(raw, addresses...)
			if err != nil {
				return nil, err
			}
			envelope[key] = masked
		}
	}
	return common.Marshal(envelope)
}
