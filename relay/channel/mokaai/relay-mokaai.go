package mokaai

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func embeddingRequestOpenAI2Moka(request dto.GeneralOpenAIRequest) *dto.EmbeddingRequest {
	var input []string // Change input to []string

	switch v := request.Input.(type) {
	case string:
		input = []string{v} // Convert string to []string
	case []string:
		input = v // Already a []string, no conversion needed
	case []interface{}:
		for _, part := range v {
			if str, ok := part.(string); ok {
				input = append(input, str) // Append each string to the slice
			}
		}
	}
	return &dto.EmbeddingRequest{
		Input: input,
		Model: request.Model,
	}
}

func embeddingResponseMoka2OpenAI(response *dto.EmbeddingResponse) *dto.OpenAIEmbeddingResponse {
	openAIEmbeddingResponse := dto.OpenAIEmbeddingResponse{
		Object: "list",
		Data:   make([]dto.OpenAIEmbeddingResponseItem, 0, len(response.Data)),
		Model:  response.Model,
		Usage:  response.Usage,
	}
	for _, item := range response.Data {
		openAIEmbeddingResponse.Data = append(openAIEmbeddingResponse.Data, dto.OpenAIEmbeddingResponseItem{
			Object:    item.Object,
			Index:     item.Index,
			Embedding: item.Embedding,
		})
	}
	return &openAIEmbeddingResponse
}

func mokaEmbeddingHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var baiduResponse dto.EmbeddingResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	err = common.Unmarshal(responseBody, &baiduResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if err := helper.ValidateEmbeddingResponse(responseBody, helper.EmbeddingInputCount(info.Request)); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if err := helper.ValidateVectorUsage(&baiduResponse.Usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	baiduResponse.Usage.UsageSource = "provider"
	fullTextResponse := embeddingResponseMoka2OpenAI(&baiduResponse)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	return &fullTextResponse.Usage, helper.WriteVectorResponse(c, resp, info, jsonResponse)
}
