package openai

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

func OaiResponsesHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	var responsesResponse dto.OpenAIResponsesResponse
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}
	if err = common.Unmarshal(responseBody, &responsesResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if oaiError := responsesResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	terminal := relayconvert.TerminationFromResponse(&responsesResponse)
	c.Set("response_termination", terminal)
	if terminal.Kind == types.ResponseFailed || terminal.Kind == types.ResponseCancelled {
		return relayconvert.UsageFromResponsesUsage(responsesResponse.Usage), types.NewOpenAIError(fmt.Errorf("upstream response %s", terminal.Kind), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	service.IOCopyBytesGracefully(c, resp, responseBody)
	usage := relayconvert.UsageFromResponsesUsage(responsesResponse.Usage)
	for _, output := range responsesResponse.Output {
		switch output.Type {
		case dto.BuildInCallWebSearchCall, dto.BuildInCallFileSearchCall, dto.BuildInCallFunctionCall:
			info.CountBillableToolCall(output.Type, output.Name)
		}
	}

	imageCounter := &relaycommon.ImageGenerationCallCounter{}
	if !relaycommon.IsNonBillableResponsesStatus(responsesResponse.Status) {
		for i := range responsesResponse.Output {
			imageCounter.Observe(&responsesResponse.Output[i], &i)
		}
	}
	imageCounter.Commit(info)
	return usage, nil
}

func OaiResponsesStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		logger.LogError(c, "invalid response or response body")
		return nil, types.NewError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse)
	}
	defer service.CloseResponseBodyGracefully(resp)

	accounting := service.NewResponsesUsageAccumulator(info)
	var streamErr *types.NewAPIError
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var event dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
			sr.Error(err)
			return
		}
		accounting.Observe(&event)
		terminal := relayconvert.TerminationFromResponse(&event)
		if terminal.IsTerminal() {
			c.Set("response_termination", terminal)
		}
		failed := terminal.Kind == types.ResponseFailed || terminal.Kind == types.ResponseCancelled || event.Type == "error"
		if !failed || c.Writer.Written() {
			// Parsing has succeeded, and native Responses forwards this semantic
			// event immediately. Image bytes must not consume the prelude budget.
			if semantic, _ := helper.StreamPayloadState(data); semantic && !failed {
				if err := helper.CommitStreamOutput(c); err != nil {
					sr.Stop(err)
					return
				}
			}
			if _, err := helper.WriteResponseChunkData(c, event, data); err != nil {
				sr.Stop(err)
				return
			}
		}
		if failed {
			streamErr = types.NewOpenAIError(fmt.Errorf("upstream responses stream ended: %s", event.Type), types.ErrorCodeBadResponse, http.StatusBadGateway)
			sr.Stop(streamErr)
		} else if terminal.IsTerminal() {
			sr.Done()
		}
	})
	usage := accounting.Finish()
	if streamErr != nil {
		return usage, streamErr
	}
	return usage, helper.StreamError(info)
}
