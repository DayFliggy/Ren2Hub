package openai

import (
	"fmt"
	"io"
	"net/http"
	"time"

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

func OaiResponsesToChatHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	var responsesResp dto.OpenAIResponsesResponse
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	if err := common.Unmarshal(body, &responsesResp); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := responsesResp.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		c.Set("response_termination", relayconvert.TerminationFromResponse(&responsesResp))
		return relayconvert.UsageFromResponsesUsage(responsesResp.Usage), types.WithOpenAIError(*oaiError, http.StatusBadGateway)
	}

	return writeConvertedResponsesResponse(c, info, resp, &responsesResp)
}

// Both JSON and buffered SSE use the same response semantics and loss policy.
func writeConvertedResponsesResponse(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response, response *dto.OpenAIResponsesResponse) (*dto.Usage, *types.NewAPIError) {
	terminal := relayconvert.TerminationFromResponse(response)
	c.Set("response_termination", terminal)
	usage := relayconvert.UsageFromResponsesUsage(response.Usage)
	if usage == nil || usage.TotalTokens == 0 {
		usage = service.ResponseText2Usage(c, service.ExtractOutputTextFromResponses(response), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if terminal.Kind == types.ResponseFailed || terminal.Kind == types.ResponseCancelled {
		return usage, types.NewOpenAIError(fmt.Errorf("upstream response %s", terminal.Kind), types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	result, err := relayconvert.ConvertResponse(c, info, info.RelayFormat, response)
	if err != nil {
		return usage, helper.ResponseConversionError(err)
	}
	helper.ObserveResponseConversion(c, *result)
	if chat, ok := result.Value.(*dto.OpenAITextResponse); ok {
		if id := helper.GetResponseID(c); id != "" {
			chat.Id = id
		}
		chat.Usage = *usage
	}
	body, err := common.Marshal(result.Value)
	if err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
	}
	service.IOCopyBytesGracefully(c, resp, body)
	return usage, nil
}

func OaiResponsesToChatBufferedStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)

	accumulator := relayconvert.NewResponsesBufferedAccumulator(helper.ResponseConversionByteLimit())
	var finalResponse *dto.OpenAIResponsesResponse
	var streamErr *types.NewAPIError

	readErr := helper.ReadSSEEvents(resp.Body, func(data, eventName string) (bool, error) {
		if data == "[DONE]" {
			return true, nil
		}
		var event dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &event); err != nil {
			return true, err
		}
		if event.Type == "" {
			event.Type = eventName
		}
		accumulator.ProcessEvent(&event)
		if err := accumulator.Err(); err != nil {
			return true, err
		}
		terminal := relayconvert.TerminationFromResponse(&event)
		if terminal.IsTerminal() {
			c.Set("response_termination", terminal)
		}
		if !terminal.IsTerminal() {
			return false, nil
		}
		finalResponse = event.Response
		if finalResponse == nil {
			finalResponse = &dto.OpenAIResponsesResponse{}
		}
		if event.Type == "response.incomplete" && len(finalResponse.Status) == 0 {
			finalResponse.Status = []byte(`"incomplete"`)
		}
		if !terminal.IsSuccessful() {
			if err := relayconvert.ValidateResponseTermination(types.RelayFormatOpenAIResponses, info.RelayFormat, terminal); err != nil && (terminal.Kind == types.ResponseUnknown || terminal.Kind == types.ResponseProviderPause) {
				streamErr = helper.ResponseConversionError(err)
			} else {
				streamErr = types.NewOpenAIError(fmt.Errorf("upstream responses stream failed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
			}
		}
		return true, nil
	})
	if readErr != nil {
		streamErr = types.NewOpenAIError(readErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
		if accumulator.Err() != nil {
			streamErr = helper.ResponseConversionError(accumulator.Err())
		}
	}
	if finalResponse == nil {
		finalResponse = &dto.OpenAIResponsesResponse{
			ID:        helper.GetResponseID(c),
			CreatedAt: int(time.Now().Unix()),
			Model:     info.UpstreamModelName,
			Status:    []byte(`"incomplete"`),
		}
		if streamErr == nil {
			streamErr = types.NewOpenAIError(io.ErrUnexpectedEOF, types.ErrorCodeBadResponse, http.StatusBadGateway)
		}
	}
	accumulator.SupplementResponseOutput(finalResponse)
	if streamErr == nil && accumulator.Err() != nil {
		streamErr = helper.ResponseConversionError(accumulator.Err())
	}
	if streamErr != nil {
		usage := service.ResponseText2Usage(c, service.ExtractOutputTextFromResponses(finalResponse), info.UpstreamModelName, info.GetEstimatePromptTokens())
		if len(finalResponse.Output) == 0 {
			usage = &dto.Usage{}
		}
		if finalResponse.Usage != nil {
			usage = relayconvert.UsageFromResponsesUsage(finalResponse.Usage)
		}
		return usage, streamErr
	}

	return writeConvertedResponsesResponse(c, info, resp, finalResponse)
}

func OaiResponsesToChatStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	if resp == nil || resp.Body == nil {
		return nil, types.NewOpenAIError(fmt.Errorf("invalid response"), types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}

	defer service.CloseResponseBodyGracefully(resp)

	responseId := helper.GetResponseID(c)
	createAt := time.Now().Unix()
	state, err := relayconvert.NewResponseStreamState(types.RelayFormatOpenAIResponses, info.RelayFormat, relayconvert.ResponseStreamOptions{MaxInputBytes: helper.ResponseConversionByteLimit(),
		ID:      responseId,
		Model:   info.UpstreamModelName,
		Created: createAt,
	})
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
	}
	streamErr := (*types.NewAPIError)(nil)

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo == nil {
		info.ClaudeConvertInfo = &relaycommon.ClaudeConvertInfo{LastMessagesType: relaycommon.LastMessageTypeNone}
	}

	sendGeminiResponse := func(geminiResponse *dto.GeminiChatResponse) bool {
		if geminiResponse == nil {
			return true
		}
		geminiResponseStr, err := common.Marshal(geminiResponse)
		if err != nil {
			streamErr = types.NewOpenAIError(err, types.ErrorCodeJsonMarshalFailed, http.StatusInternalServerError)
			return false
		}
		c.Render(-1, common.CustomEvent{Data: "data: " + string(geminiResponseStr)})
		_ = helper.FlushWriter(c)
		return true
	}

	sendStreamResult := func(result relayconvert.ResponseResult) bool {
		switch value := result.Value.(type) {
		case dto.ChatCompletionsStreamResponse:
			if len(value.Choices) == 0 && value.Usage == nil {
				return true
			}
			if err := helper.ObjectData(c, &value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case *dto.ChatCompletionsStreamResponse:
			if value == nil || (len(value.Choices) == 0 && value.Usage == nil) {
				return true
			}
			if err := helper.ObjectData(c, value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case dto.ClaudeResponse:
			if err := helper.ClaudeData(c, value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case *dto.ClaudeResponse:
			if value == nil {
				return true
			}
			if err := helper.ClaudeData(c, *value); err != nil {
				streamErr = types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
				return false
			}
			return true
		case dto.GeminiChatResponse:
			return sendGeminiResponse(&value)
		case *dto.GeminiChatResponse:
			return sendGeminiResponse(value)
		default:
			streamErr = types.NewOpenAIError(fmt.Errorf("unsupported converted stream response type %T", result.Value), types.ErrorCodeBadResponse, http.StatusInternalServerError)
			return false
		}
	}

	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		if streamErr != nil {
			sr.Stop(streamErr)
			return
		}

		var streamResp dto.ResponsesStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResp); err != nil {
			logger.LogError(c, "failed to unmarshal responses stream event: "+err.Error())
			sr.Error(err)
			return
		}

		terminal := relayconvert.TerminationFromResponse(&streamResp)
		if terminal.IsTerminal() {
			c.Set("response_termination", terminal)
		}
		if terminal.IsTerminal() && !terminal.IsSuccessful() {
			if streamResp.Response != nil && streamResp.Response.Usage != nil {
				state.SetUsage(relayconvert.UsageFromResponsesUsage(streamResp.Response.Usage))
			}
			if terminal.Kind == types.ResponseUnknown || terminal.Kind == types.ResponseProviderPause {
				streamErr = helper.ResponseConversionError(relayconvert.ValidateResponseTermination(types.RelayFormatOpenAIResponses, info.RelayFormat, terminal))
				sr.Stop(streamErr)
				return
			}
			if streamResp.Response != nil {
				if oaiErr := streamResp.Response.GetOpenAIError(); oaiErr != nil && oaiErr.Type != "" {
					streamErr = types.WithOpenAIError(*oaiErr, http.StatusBadGateway)
					sr.Stop(streamErr)
					return
				}
			}
			streamErr = types.NewOpenAIError(fmt.Errorf("responses stream error: %s", streamResp.Type), types.ErrorCodeBadResponse, http.StatusBadGateway)
			sr.Stop(streamErr)
			return
		}

		results, err := relayconvert.ConvertStreamResponseChunk(c, info, state, &streamResp)
		helper.ObserveResponseStream(c, state)
		if err != nil {
			streamErr = helper.ResponseConversionError(err)
			sr.Stop(streamErr)
			return
		}
		for _, result := range results {
			helper.ObserveResponseConversion(c, result)
			if !sendStreamResult(result) {
				sr.Stop(streamErr)
				return
			}
		}
		if terminal.IsSuccessful() {
			sr.Done()
		}
	})

	usage := state.Usage()
	if (usage == nil || usage.TotalTokens == 0) && state.UsageText() != "" {
		usage = service.ResponseText2Usage(c, state.UsageText(), info.UpstreamModelName, info.GetEstimatePromptTokens())
		state.SetUsage(usage)
	}

	if info.RelayFormat == types.RelayFormatClaude && info.ClaudeConvertInfo != nil {
		info.ClaudeConvertInfo.Usage = usage
	}
	finalErr := helper.FinalizeConvertedStream(c, info, state)
	if streamErr != nil {
		return usage, streamErr
	}
	if finalErr != nil {
		return usage, finalErr
	}
	if info.RelayFormat == types.RelayFormatOpenAI && info.ShouldIncludeUsage && usage != nil {
		if err := helper.ObjectData(c, helper.GenerateFinalUsageResponse(responseId, createAt, info.UpstreamModelName, *usage)); err != nil {
			return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusInternalServerError)
		}
	}

	if info.RelayFormat == types.RelayFormatOpenAI {
		helper.Done(c)
	}
	return usage, nil
}
