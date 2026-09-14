package relay

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func AudioHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	audioReq, ok := info.Request.(*dto.AudioRequest)
	if !ok || audioReq == nil {
		return types.NewError(errors.New("invalid request type"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if info.RelayMode == relayconstant.RelayModeAudioSpeech {
		if strings.TrimSpace(audioReq.Input) == "" || strings.TrimSpace(audioReq.Voice) == "" {
			return types.NewErrorWithStatusCode(errors.New("input and voice are required"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		if audioReq.StreamFormat != "" && audioReq.StreamFormat != "sse" && audioReq.StreamFormat != "audio" {
			return types.NewErrorWithStatusCode(errors.New("invalid stream_format"), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
	}

	request, err := common.DeepCopy(audioReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to AudioRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	ioReader, err := adaptor.ConvertAudioRequest(c, info, *request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	resp, err := adaptor.DoRequest(c, info, ioReader)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	statusCodeMappingStr := c.GetString("status_code_mapping")

	httpResp, ok := resp.(*http.Response)
	if !ok || httpResp == nil || httpResp.Body == nil {
		return types.NewOpenAIError(errors.New("invalid audio upstream response"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	if httpResp.StatusCode != http.StatusOK {
		newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	observed, usageOK := usage.(*dto.Usage)
	if usageOK && observed != nil {
		kind := "text"
		if observed.CompletionTokenDetails.AudioTokens > 0 || observed.PromptTokensDetails.AudioTokens > 0 {
			kind = "audio"
		}
		info.CaptureResponseUsage(observed, kind, nil)
	}
	if newAPIError == nil {
		newAPIError = helper.StreamError(info)
	}
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}
	if !usageOK || observed == nil {
		return types.NewOpenAIError(errors.New("audio adapter returned no usage"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}

	return nil
}
