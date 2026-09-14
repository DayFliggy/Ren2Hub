package dify

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/samber/lo"

	"github.com/gin-gonic/gin"
)

func requestOpenAI2Dify(c *gin.Context, request dto.GeneralOpenAIRequest) (*DifyChatRequest, error) {
	difyReq := DifyChatRequest{
		Inputs:           make(map[string]interface{}),
		AutoGenerateName: false,
	}

	stringUser := helper.GetResponseID(c)
	if len(request.User) > 0 {
		if err := common.Unmarshal(request.User, &stringUser); err != nil {
			return nil, fmt.Errorf("dify user must be a string: %w", err)
		}
	}
	difyReq.User = stringUser

	files := make([]DifyFile, 0)
	var content strings.Builder
	for _, message := range request.Messages {
		if message.Role == "system" {
			content.WriteString("SYSTEM: \n" + message.StringContent() + "\n")
		} else if message.Role == "assistant" {
			content.WriteString("ASSISTANT: \n" + message.StringContent() + "\n")
		} else {
			parseContent := message.ParseContent()
			for _, mediaContent := range parseContent {
				switch mediaContent.Type {
				case dto.ContentTypeText:
					if len(request.Messages) == 1 && message.Role == "user" {
						content.WriteString(mediaContent.Text)
					} else {
						content.WriteString("USER: \n" + mediaContent.Text + "\n")
					}
				case dto.ContentTypeImageURL:
					media := mediaContent.GetImageMedia()
					if media == nil || media.Url == "" {
						return nil, fmt.Errorf("dify image is missing its URL")
					}
					file := DifyFile{Type: "image", TransferMode: "remote_url", URL: media.Url}
					if !media.IsRemoteImage() {
						if _, _, err := decodeDifyImage(media.Url); err != nil {
							return nil, err
						}
						file.TransferMode = "gateway_inline"
					}
					files = append(files, file)
				}
			}
		}
	}
	difyReq.Query = content.String()
	difyReq.Files = files
	mode := "blocking"
	if lo.FromPtrOr(request.Stream, false) {
		mode = "streaming"
	}
	difyReq.ResponseMode = mode
	return &difyReq, nil
}

func streamResponseDify2OpenAI(difyResponse DifyChunkChatCompletionResponse) *dto.ChatCompletionsStreamResponse {
	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Created: common.GetTimestamp(),
		Model:   "dify",
	}
	var choice dto.ChatCompletionsStreamResponseChoice
	if strings.HasPrefix(difyResponse.Event, "workflow_") {
		if constant.DifyDebug {
			text := "Workflow: " + difyResponse.Data.WorkflowId
			if difyResponse.Event == "workflow_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if strings.HasPrefix(difyResponse.Event, "node_") {
		if constant.DifyDebug {
			text := "Node: " + difyResponse.Data.NodeType
			if difyResponse.Event == "node_finished" {
				text += " " + difyResponse.Data.Status
			}
			choice.Delta.SetReasoningContent(text + "\n")
		}
	} else if difyResponse.Event == "message" || difyResponse.Event == "agent_message" {
		if difyResponse.Answer == "<details style=\"color:gray;background-color: #f8f8f8;padding: 8px;border-radius: 4px;\" open> <summary> Thinking... </summary>\n" {
			difyResponse.Answer = "<think>"
		} else if difyResponse.Answer == "</details>" {
			difyResponse.Answer = "</think>"
		}

		choice.Delta.SetContentString(difyResponse.Answer)
	}
	response.Choices = append(response.Choices, choice)
	return &response
}

func difyStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var responseText string
	usage := &dto.Usage{}
	responseID, created := helper.GetResponseID(c), common.GetTimestamp()
	sentRole := false
	helper.SetEventStreamHeaders(c)
	helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) {
		var difyResponse DifyChunkChatCompletionResponse
		if err := common.Unmarshal([]byte(data), &difyResponse); err != nil {
			common.SysLog("error unmarshalling stream response: " + err.Error())
			sr.Error(err)
			return
		}
		if difyResponse.Event == "message_end" {
			if err := service.NormalizeProviderUsage(&difyResponse.MetaData.Usage); err != nil {
				sr.Stop(err)
				return
			}
			usage = &difyResponse.MetaData.Usage
			usage.UsageSource = "provider"
			finish := "stop"
			response := dto.ChatCompletionsStreamResponse{Id: responseID, Created: created, Object: "chat.completion.chunk", Model: info.UpstreamModelName,
				Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0, FinishReason: &finish}}}
			if !sentRole {
				response.Choices[0].Delta.Role = "assistant"
			}
			if err := helper.ObjectData(c, response); err != nil {
				sr.Stop(err)
				return
			}
			sr.Done()
			return
		} else if difyResponse.Event == "error" {
			sr.Stop(fmt.Errorf("dify error event"))
			return
		}
		openaiResponse := *streamResponseDify2OpenAI(difyResponse)
		openaiResponse.Id, openaiResponse.Created, openaiResponse.Model = responseID, created, info.UpstreamModelName
		if len(openaiResponse.Choices) != 0 {
			if !sentRole {
				openaiResponse.Choices[0].Delta.Role = "assistant"
				sentRole = true
			}
			responseText += openaiResponse.Choices[0].Delta.GetContentString()
			responseText += openaiResponse.Choices[0].Delta.GetReasoningContent()
			if openaiResponse.Choices[0].Delta.GetContentString() != "" || openaiResponse.Choices[0].Delta.GetReasoningContent() != "" {
				sr.MarkValidOutput()
			}
		}
		if err := helper.ObjectData(c, openaiResponse); err != nil {
			common.SysLog(err.Error())
			sr.Error(err)
		}
	})
	if usage.TotalTokens == 0 && (responseText != "" || helper.StreamError(info) == nil) {
		usage = service.ResponseText2Usage(c, responseText, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if streamErr := helper.StreamError(info); streamErr != nil {
		return usage, streamErr
	}
	helper.Done(c)
	return usage, nil
}

func difyHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	var difyResponse DifyChatCompletionResponse
	responseBody, err := io.ReadAll(resp.Body)

	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	err = common.Unmarshal(responseBody, &difyResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	if difyResponse.Event == "error" || difyResponse.Code != "" {
		return nil, types.NewErrorWithStatusCode(fmt.Errorf("dify generation failed"), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if err := service.NormalizeProviderUsage(&difyResponse.MetaData.Usage); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if !service.ValidUsage(&difyResponse.MetaData.Usage) {
		difyResponse.MetaData.Usage = *service.ResponseText2Usage(c, difyResponse.Answer, info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	fullTextResponse := dto.OpenAITextResponse{
		Id:      difyResponse.ConversationId,
		Object:  "chat.completion",
		Created: common.GetTimestamp(),
		Usage:   difyResponse.MetaData.Usage,
	}
	choice := dto.OpenAITextResponseChoice{
		Index: 0,
		Message: dto.Message{
			Role:    "assistant",
			Content: difyResponse.Answer,
		},
		FinishReason: "stop",
	}
	fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	jsonResponse, err := common.Marshal(fullTextResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	c.Writer.Write(jsonResponse)
	return &difyResponse.MetaData.Usage, nil
}
