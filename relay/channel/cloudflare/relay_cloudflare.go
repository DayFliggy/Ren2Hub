package cloudflare

import (
	"bytes"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func convertCf2CompletionsRequest(textRequest dto.GeneralOpenAIRequest) *CfRequest {
	p, _ := textRequest.Prompt.(string)
	return &CfRequest{
		Prompt:      p,
		MaxTokens:   textRequest.GetMaxTokens(),
		Stream:      lo.FromPtrOr(textRequest.Stream, false),
		Temperature: textRequest.Temperature,
	}
}

func cfStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	usage, err := openai.OaiStreamHandler(c, info, resp)
	return err, usage
}

func cfHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	if info.RelayMode == relayconstant.RelayModeChatCompletions {
		usage, err := openai.OpenaiHandler(c, info, resp)
		return err, usage
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	service.CloseResponseBodyGracefully(resp)
	var response dto.TextResponse
	err = common.Unmarshal(responseBody, &response)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	response.Model = info.UpstreamModelName
	var responseText string
	for _, choice := range response.Choices {
		responseText += choice.Message.StringContent()
	}
	usage := service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	response.Usage = *usage
	response.Id = helper.GetResponseID(c)
	jsonResponse, err := common.Marshal(response)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return nil, usage
}

func cfSTTHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*types.NewAPIError, *dto.Usage) {
	if resp == nil || resp.Body == nil {
		return types.NewOpenAIError(fmt.Errorf("invalid Cloudflare transcription response"), types.ErrorCodeBadResponse, http.StatusBadGateway), nil
	}
	defer service.CloseResponseBodyGracefully(resp)
	var cfResp CfAudioResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusBadGateway), nil
	}
	err = common.Unmarshal(responseBody, &cfResp)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
	}

	if cfResp.Success != nil && !*cfResp.Success || len(cfResp.Errors) > 0 {
		return types.NewOpenAIError(fmt.Errorf("Cloudflare transcription failed"), types.ErrorCodeBadResponse, http.StatusBadGateway), nil
	}
	if cfResp.Result == nil || cfResp.Result.Text == nil {
		return types.NewOpenAIError(fmt.Errorf("Cloudflare transcription response is missing text"), types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
	}
	jsonResponse, err := common.Marshal(&dto.AudioResponse{Text: *cfResp.Result.Text})
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	format := c.GetString("response_format")
	contentType := "application/json"
	if format == "text" {
		jsonResponse, contentType = []byte(*cfResp.Result.Text), "text/plain"
	}
	return openai.OpenaiSTTHandler(c, &http.Response{StatusCode: resp.StatusCode, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(bytes.NewReader(jsonResponse))}, info, format)
}
