package gemini

import (
	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"net/http"
)

func GeminiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	_, response, apiErr := readGeminiGenerationResponse(c, resp)
	if apiErr != nil {
		return nil, apiErr
	}
	markGeminiGoogleSearchCall(c, response)
	usage := buildUsageFromGeminiResponse(c, info, response)
	converted, err := relayconvert.ConvertResponse(c, info, types.RelayFormatOpenAIResponses, response)
	if err != nil {
		return &usage, helper.ResponseConversionError(err)
	}
	helper.ObserveResponseConversion(c, *converted)
	output := converted.Value.(*dto.OpenAIResponsesResponse)
	output.Model = info.UpstreamModelName
	output.ID = helper.GetResponseID(c)
	output.Usage = relayconvert.UsageFromChatUsage(&usage)
	body, err := common.Marshal(output)
	if err != nil {
		return &usage, types.NewError(err, types.ErrorCodeJsonMarshalFailed)
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return &usage, nil
}

func GeminiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	return geminiConvertedStreamHandler(c, info, resp, types.RelayFormatOpenAIResponses)
}

func geminiConvertedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, target types.RelayFormat) (*dto.Usage, *types.NewAPIError) {
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatGemini, target, relayconvert.ResponseStreamOptions{ID: helper.GetResponseID(c), Model: info.UpstreamModelName, Created: common.GetTimestamp(), IncludeUsage: info.ShouldIncludeUsage})
	if err != nil {
		return nil, helper.ResponseConversionError(err)
	}
	var conversionErr *types.NewAPIError
	usage, streamErr := geminiStreamHandler(c, info, resp, func(_ string, response *dto.GeminiChatResponse) bool {
		results, err := relayconvert.ConvertStreamResponseChunk(c, info, state, response)
		if err != nil {
			conversionErr = helper.ResponseConversionError(err)
			return false
		}
		for _, result := range results {
			if err := helper.WriteConvertedStreamResult(c, result); err != nil {
				conversionErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
				return false
			}
		}
		return true
	})
	if usage != nil {
		state.SetUsage(usage)
	}
	if conversionErr != nil {
		return usage, conversionErr
	}
	finalErr := helper.FinalizeConvertedStream(c, info, state)
	if streamErr != nil {
		return usage, streamErr
	}
	if finalErr != nil {
		return usage, finalErr
	}
	if target == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}
