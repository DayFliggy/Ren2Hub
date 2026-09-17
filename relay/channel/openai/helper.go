package openai

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

// 辅助函数
func HandleStreamFormat(c *gin.Context, info *relaycommon.RelayInfo, data string, forceFormat bool, thinkToContent bool) error {
	info.SendResponseCount++

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		return sendStreamData(c, info, data, forceFormat, thinkToContent)
	case types.RelayFormatClaude:
		return handleClaudeFormat(c, data, info)
	case types.RelayFormatGemini:
		return handleGeminiFormat(c, data, info)
	}
	return nil
}

func handleClaudeFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		return err
	}

	if streamResponse.Usage != nil {
		info.EnsureClaudeConvertInfo().Usage = streamResponse.Usage
	}
	result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatClaude, &streamResponse)
	if err != nil {
		return err
	}
	claudeResponses, ok := result.Value.([]*dto.ClaudeResponse)
	if !ok {
		return fmt.Errorf("expected Claude stream responses, got %T", result.Value)
	}
	for _, resp := range claudeResponses {
		if err := helper.ClaudeData(c, *resp); err != nil {
			return err
		}
	}
	return nil
}

func handleGeminiFormat(c *gin.Context, data string, info *relaycommon.RelayInfo) error {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.Unmarshal(common.StringToByteSlice(data), &streamResponse); err != nil {
		logger.LogError(c, "failed to unmarshal stream response: "+err.Error())
		return err
	}

	result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatGemini, &streamResponse)
	if err != nil {
		return err
	}
	geminiResponse, ok := result.Value.(*dto.GeminiChatResponse)
	if !ok {
		return fmt.Errorf("expected Gemini stream response, got %T", result.Value)
	}

	// 如果返回 nil，表示没有实际内容，跳过发送
	if geminiResponse == nil {
		return nil
	}

	geminiResponseStr, err := common.Marshal(geminiResponse)
	if err != nil {
		logger.LogError(c, "failed to marshal gemini response: "+err.Error())
		return err
	}

	semantic, terminal := helper.StreamPayloadState(string(geminiResponseStr))
	if !semantic && !terminal {
		return nil
	}
	if err := helper.StringData(c, string(geminiResponseStr)); err != nil {
		return err
	}
	if semantic && c.Writer.Written() {
		info.SetFirstResponseTime()
		info.MarkValidOutput()
	}
	return nil
}

func ProcessStreamResponse(streamResponse dto.ChatCompletionsStreamResponse, responseTextBuilder *strings.Builder, toolCount *int) error {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Delta.GetContentString())
		responseTextBuilder.WriteString(choice.Delta.GetReasoningContent())
		if choice.Delta.ToolCalls != nil {
			if len(choice.Delta.ToolCalls) > *toolCount {
				*toolCount = len(choice.Delta.ToolCalls)
			}
			for _, tool := range choice.Delta.ToolCalls {
				responseTextBuilder.WriteString(tool.Function.Name)
				responseTextBuilder.WriteString(tool.Function.Arguments)
			}
		}
	}
	return nil
}

func processTokenData(relayMode int, data string, responseTextBuilder *strings.Builder, toolCount *int) error {
	switch relayMode {
	case relayconstant.RelayModeChatCompletions:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		return ProcessStreamResponse(streamResponse, responseTextBuilder, toolCount)
	case relayconstant.RelayModeCompletions:
		var streamResponse dto.CompletionsStreamResponse
		if err := common.UnmarshalJsonStr(data, &streamResponse); err != nil {
			return err
		}
		processCompletionsStreamResponse(streamResponse, responseTextBuilder)
	}
	return nil
}

func processCompletionsStreamResponse(streamResponse dto.CompletionsStreamResponse, responseTextBuilder *strings.Builder) {
	for _, choice := range streamResponse.Choices {
		responseTextBuilder.WriteString(choice.Text)
	}
}

// Only identity and per-choice termination are retained; content is never replayed.
type chatStreamTail struct {
	response dto.ChatCompletionsStreamResponse
}

func (tail *chatStreamTail) observe(response dto.ChatCompletionsStreamResponse) {
	if response.Id != "" {
		tail.response.Id = response.Id
	}
	if response.Created != 0 {
		tail.response.Created = response.Created
	}
	if response.Model != "" {
		tail.response.Model = response.Model
	}
	if response.SystemFingerprint != nil {
		tail.response.SystemFingerprint = response.SystemFingerprint
	}
	for _, choice := range response.Choices {
		index := -1
		for i := range tail.response.Choices {
			if tail.response.Choices[i].Index == choice.Index {
				index = i
				break
			}
		}
		if index < 0 {
			index = len(tail.response.Choices)
			tail.response.Choices = append(tail.response.Choices, dto.ChatCompletionsStreamResponseChoice{Index: choice.Index})
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			tail.response.Choices[index].FinishReason = choice.FinishReason
		}
	}
}

func (tail *chatStreamTail) finalData(usage *dto.Usage) (string, error) {
	tail.response.Usage = usage
	for i := range tail.response.Choices {
		if tail.response.Choices[i].FinishReason == nil {
			stop := "stop"
			tail.response.Choices[i].FinishReason = &stop
		}
	}
	encoded, err := common.Marshal(tail.response)
	return string(encoded), err
}

func HandleFinalResponse(c *gin.Context, info *relaycommon.RelayInfo, lastStreamData string,
	responseId string, createAt int64, model string, systemFingerprint string,
	usage *dto.Usage, containStreamUsage bool) error {

	switch info.RelayFormat {
	case types.RelayFormatOpenAI:
		if info.ShouldIncludeUsage && !containStreamUsage {
			response := helper.GenerateFinalUsageResponse(responseId, createAt, model, *usage)
			response.SetSystemFingerprint(systemFingerprint)
			if err := helper.ObjectData(c, response); err != nil {
				return err
			}
		}
		return helper.StringData(c, "[DONE]")

	case types.RelayFormatClaude:
		var streamResponse dto.ChatCompletionsStreamResponse
		if err := common.Unmarshal(common.StringToByteSlice(lastStreamData), &streamResponse); err != nil {
			return err
		}

		info.EnsureClaudeConvertInfo().Usage = usage

		result, err := relayconvert.ConvertStreamResponse(c, info, types.RelayFormatClaude, &streamResponse)
		if err != nil {
			return err
		}
		claudeResponses, ok := result.Value.([]*dto.ClaudeResponse)
		if !ok {
			return fmt.Errorf("expected Claude stream responses, got %T", result.Value)
		}
		for _, resp := range claudeResponses {
			if err := helper.ClaudeData(c, *resp); err != nil {
				return err
			}
		}
		info.ClaudeConvertInfo.Done = true

	case types.RelayFormatGemini:
		return handleGeminiFormat(c, lastStreamData, info)
	}
	return nil
}
