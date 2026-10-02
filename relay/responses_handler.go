package relay

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"

	"github.com/gin-gonic/gin"
)

var responsesCompactAllowedFields = []string{
	"model",
	"input",
	"instructions",
	"previous_response_id",
	"parallel_tool_calls",
	"prompt_cache_key",
	"prompt_cache_options",
	"prompt_cache_retention",
	"service_tier",
}

func responsesCompactUpstreamRequest(req *dto.OpenAIResponsesCompactionRequest) *dto.OpenAIResponsesRequest {
	return &dto.OpenAIResponsesRequest{
		Model:                req.Model,
		Input:                req.Input,
		Instructions:         req.Instructions,
		PreviousResponseID:   req.PreviousResponseID,
		ParallelToolCalls:    req.ParallelToolCalls,
		ServiceTier:          req.ServiceTier,
		PromptCacheKey:       req.PromptCacheKey,
		PromptCacheOptions:   req.PromptCacheOptions,
		PromptCacheRetention: req.PromptCacheRetention,
	}
}

func compactRequestDebugSummary(info *relaycommon.RelayInfo, jsonData []byte) string {
	var payload map[string]json.RawMessage
	present := make([]string, 0, len(responsesCompactAllowedFields))
	if err := common.Unmarshal(jsonData, &payload); err == nil {
		for _, field := range responsesCompactAllowedFields {
			if _, ok := payload[field]; ok {
				present = append(present, field)
			}
		}
	}
	return fmt.Sprintf(
		"body_bytes=%d fields=%s requested_model=%q upstream_model=%q",
		len(jsonData),
		strings.Join(present, ","),
		info.OriginModelName,
		info.ActualUpstreamModelName(),
	)
}

func remoteCompactionRequestDebugSummary(info *relaycommon.RelayInfo, jsonData []byte) string {
	var request dto.OpenAIResponsesRequest
	_ = common.Unmarshal(jsonData, &request)
	return fmt.Sprintf(
		"body_bytes=%d context_management=%t compaction_trigger=%t compaction_item=%t requested_model=%q upstream_model=%q",
		len(jsonData),
		request.RequiresNativeResponses() && len(request.ContextManagement) > 0,
		request.HasCompactionTrigger(),
		request.HasCompactionItem(),
		info.OriginModelName,
		info.ActualUpstreamModelName(),
	)
}

func ResponsesHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact &&
		!common.IsResponsesCompactAPIType(info.ApiType) {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("unsupported endpoint %q for api type %d", "/v1/responses/compact", info.ApiType),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	var responsesReq *dto.OpenAIResponsesRequest
	switch req := info.Request.(type) {
	case *dto.OpenAIResponsesRequest:
		responsesReq = req
	case *dto.OpenAIResponsesCompactionRequest:
		// Only fields documented for POST /v1/responses/compact are forwarded:
		// model, input, instructions, previous_response_id, parallel_tool_calls, prompt_cache_key,
		// prompt_cache_options, prompt_cache_retention, service_tier.
		// Undocumented Codex-parity fields (tools, reasoning, text) are parsed
		// for client compatibility but intentionally not sent upstream.
		responsesReq = responsesCompactUpstreamRequest(req)
	default:
		return types.NewErrorWithStatusCode(
			fmt.Errorf("invalid request type, expected dto.OpenAIResponsesRequest or dto.OpenAIResponsesCompactionRequest, got %T", info.Request),
			types.ErrorCodeInvalidRequest,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}

	adaptor, requestBody, closer, prepareErr := PrepareResponsesRequest(c, info, responsesReq)
	if prepareErr != nil {
		return prepareErr
	}
	defer closer.Close()

	var httpResp *http.Response
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	statusCodeMappingStr := c.GetString("status_code_mapping")

	if resp != nil {
		httpResp = resp.(*http.Response)

		if httpResp.StatusCode != http.StatusOK {
			newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
			// reset status code 重置状态码
			service.ResetStatusCode(newAPIError, statusCodeMappingStr)
			return newAPIError
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if observed, ok := usage.(*dto.Usage); ok {
		info.CaptureResponseUsage(observed, "text", nil)
	}
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usageDto := usage.(*dto.Usage)
	if info.RelayMode == relayconstant.RelayModeResponsesCompact {
		info.CaptureResponseUsage(usageDto, "text", nil)
		return nil
	}

	if strings.HasPrefix(info.OriginModelName, "gpt-4o-audio") {
		info.CaptureResponseUsage(usageDto, "audio", nil)
	} else {
		info.CaptureResponseUsage(usageDto, "text", nil)
	}
	return nil
}

// PrepareResponsesRequest builds the final provider payload for HTTP and WS.
// The caller owns the returned body and invokes the shared send guard before I/O.
func PrepareResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, responsesReq *dto.OpenAIResponsesRequest) (channel.Adaptor, io.Reader, io.Closer, *types.NewAPIError) {
	info.InitChannelMeta(c)
	request, err := common.DeepCopy(responsesReq)
	if err != nil {
		return nil, nil, nil, types.NewError(fmt.Errorf("failed to copy request to GeneralOpenAIRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}
	if err := helper.ApplyReasoningModelSuffix(c, info, request); err != nil {
		return nil, nil, nil, newConvertRequestFailedError(c, info, err)
	}
	if err := relaycommon.ApplyChannelSystemPrompt(c, info, request); err != nil {
		return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return nil, nil, nil, types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)
	var requestBody io.Reader
	var bodyCloser io.Closer
	passThroughBody := model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled
	if passThroughBody {
		body, closer, err := relaycommon.NativeRequestBody(c, info, request)
		if err != nil {
			return nil, nil, nil, types.NewError(err, types.ErrorCodeReadRequestBodyFailed, types.ErrOptionWithSkipRetry())
		}
		bodyCloser = closer
		requestBody = body
	} else {
		convertedRequest, err := adaptor.ConvertOpenAIResponsesRequest(c, info, *request)
		if err != nil {
			return nil, nil, nil, newConvertRequestFailedError(c, info, err)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)
		jsonData, err := common.Marshal(convertedRequest)
		if err != nil {
			return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// remove disabled fields for OpenAI Responses API
		jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
		if err != nil {
			return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}

		// apply param override
		if len(info.ParamOverride) > 0 {
			jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
			if err != nil {
				return nil, nil, nil, newAPIErrorFromParamOverride(err)
			}
		}
		if info.RelayMode == relayconstant.RelayModeResponsesCompact {
			logger.LogDebug(c, "compact request metadata: %s", compactRequestDebugSummary(info, jsonData))
		} else if common.GetContextKeyBool(c, constant.ContextKeyResponsesNativeRequired) {
			logger.LogDebug(c, "remote compaction request metadata: %s", remoteCompactionRequestDebugSummary(info, jsonData))
		} else {
			logger.LogDebug(c, "requestBody: %s", jsonData)
		}
		body, closer, err := relaycommon.NewOutboundJSONBody(jsonData)
		if err != nil {
			return nil, nil, nil, types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
		}
		bodyCloser = closer
		jsonData = nil
		requestBody = body
	}

	return adaptor, requestBody, bodyCloser, nil
}
