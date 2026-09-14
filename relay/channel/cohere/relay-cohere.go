package cohere

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/samber/lo"
)

func requestOpenAI2Cohere(textRequest dto.GeneralOpenAIRequest) *CohereRequest {
	cohereReq := CohereRequest{
		Model:       textRequest.Model,
		ChatHistory: []ChatHistory{},
		Message:     "",
		Stream:      textRequest.Stream,
		MaxTokens:   textRequest.MaxTokens,
	}
	if common.CohereSafetySetting != "NONE" {
		cohereReq.SafetyMode = common.CohereSafetySetting
	}
	if textRequest.MaxCompletionTokens != nil {
		cohereReq.MaxTokens = textRequest.MaxCompletionTokens
	}
	for index, msg := range textRequest.Messages {
		if index == len(textRequest.Messages)-1 && msg.Role == "user" {
			cohereReq.Message = msg.StringContent()
		} else {
			var role string
			if msg.Role == "assistant" {
				role = "CHATBOT"
			} else if msg.Role == "system" {
				role = "SYSTEM"
			} else {
				role = "USER"
			}
			cohereReq.ChatHistory = append(cohereReq.ChatHistory, ChatHistory{
				Role:    role,
				Message: msg.StringContent(),
			})
		}
	}

	return &cohereReq
}

func requestConvertRerank2Cohere(rerankRequest dto.RerankRequest) *CohereRerankRequest {
	topN := lo.FromPtrOr(rerankRequest.TopN, 1)
	if topN <= 0 {
		topN = 1
	}
	cohereReq := CohereRerankRequest{
		Query:           rerankRequest.Query,
		Documents:       rerankRequest.Documents,
		Model:           rerankRequest.Model,
		TopN:            topN,
		ReturnDocuments: true,
	}
	return &cohereReq
}

func stopReasonCohere2OpenAI(reason string) string {
	switch reason {
	case "COMPLETE":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	default:
		return reason
	}
}

func cohereStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	responseId := helper.GetResponseID(c)
	createdTime := common.GetTimestamp()
	var usage *dto.Usage
	var responseText strings.Builder
	sentRole := false
	helper.NDJSONStreamScannerHandler(c, resp, info, func(data string, result *helper.StreamResult) {
		var chunk CohereResponse
		if err := common.Unmarshal([]byte(data), &chunk); err != nil {
			result.Stop(err)
			return
		}
		if strings.HasPrefix(chunk.FinishReason, "ERROR") {
			result.Stop(fmt.Errorf("cohere generation failed: %s", chunk.FinishReason))
			return
		}
		response := dto.ChatCompletionsStreamResponse{
			Id: responseId, Created: createdTime, Object: "chat.completion.chunk", Model: info.UpstreamModelName,
			Choices: []dto.ChatCompletionsStreamResponseChoice{{Index: 0}},
		}
		if chunk.IsFinished {
			finishReason := stopReasonCohere2OpenAI(chunk.FinishReason)
			if finishReason == "" {
				result.Stop(fmt.Errorf("cohere final event has no finish reason"))
				return
			}
			response.Choices[0].FinishReason = &finishReason
			if chunk.Response != nil {
				units := chunk.Response.Meta.BilledUnits
				observed := &dto.Usage{PromptTokens: units.InputTokens, CompletionTokens: units.OutputTokens, UsageSource: "provider"}
				if err := service.NormalizeProviderUsage(observed); err != nil {
					result.Stop(err)
					return
				}
				usage = observed
			}
		} else if chunk.Text == "" {
			return
		} else {
			response.Choices[0].Delta.SetContentString(chunk.Text)
		}
		if !sentRole {
			response.Choices[0].Delta.Role = "assistant"
			sentRole = true
		}
		if err := helper.ObjectData(c, response); err != nil {
			result.Stop(err)
			return
		}
		if chunk.Text != "" && !chunk.IsFinished {
			result.MarkValidOutput()
			responseText.WriteString(chunk.Text)
		}
		if chunk.IsFinished {
			result.Done()
		}
	})
	if usage == nil {
		usage = service.ResponseText2Usage(c, responseText.String(), info.UpstreamModelName, info.GetEstimatePromptTokens())
	}
	if err := helper.StreamError(info); err != nil {
		return usage, err
	}
	if err := openai.HandleFinalResponse(c, info, "", responseId, createdTime, info.UpstreamModelName, "", usage, false); err != nil {
		return usage, types.NewOpenAIError(err, types.ErrorCodeBadResponse, http.StatusBadGateway)
	}
	return usage, nil
}

func cohereHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (*dto.Usage, *types.NewAPIError) {
	createdTime := common.GetTimestamp()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	var cohereResp CohereResponseResult
	err = json.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{}
	usage.PromptTokens = cohereResp.Meta.BilledUnits.InputTokens
	usage.CompletionTokens = cohereResp.Meta.BilledUnits.OutputTokens
	if err := service.NormalizeProviderUsage(&usage); err != nil {
		return nil, types.NewErrorWithStatusCode(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	var openaiResp dto.TextResponse
	openaiResp.Id = cohereResp.ResponseId
	openaiResp.Created = createdTime
	openaiResp.Object = "chat.completion"
	openaiResp.Model = info.UpstreamModelName
	openaiResp.Usage = usage

	openaiResp.Choices = []dto.OpenAITextResponseChoice{
		{
			Index:        0,
			Message:      dto.Message{Content: cohereResp.Text, Role: "assistant"},
			FinishReason: stopReasonCohere2OpenAI(cohereResp.FinishReason),
		},
	}

	jsonResponse, err := json.Marshal(openaiResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	_, _ = c.Writer.Write(jsonResponse)
	return &usage, nil
}

func cohereRerankHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	service.CloseResponseBodyGracefully(resp)
	if err := helper.ValidateRerankResponse(responseBody, len(info.Documents)); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	var cohereResp CohereRerankResponseResult
	err = common.Unmarshal(responseBody, &cohereResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	usage := dto.Usage{PromptTokens: cohereResp.Meta.BilledUnits.InputTokens, CompletionTokens: cohereResp.Meta.BilledUnits.OutputTokens,
		TotalTokens: cohereResp.Meta.BilledUnits.InputTokens + cohereResp.Meta.BilledUnits.OutputTokens, UsageSource: "provider"}
	if err := helper.ValidateVectorUsage(&usage); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}
	if usage.TotalTokens == 0 {
		usage = *service.ResponseText2Usage(c, "", info.UpstreamModelName, info.GetEstimatePromptTokens())
	}

	var rerankResp dto.RerankResponse
	rerankResp.Results = cohereResp.Results
	rerankResp.Usage = usage

	jsonResponse, err := common.Marshal(rerankResp)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeBadResponseBody)
	}
	return &usage, helper.WriteVectorResponse(c, resp, info, jsonResponse)
}
