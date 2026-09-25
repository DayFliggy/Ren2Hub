package openai

import (
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
	if info.RelayFormat == types.RelayFormatOpenAI {
		info.SendResponseCount++
		return sendStreamData(c, info, data, forceFormat, thinkToContent)
	}
	if info.RelayFormat != types.RelayFormatClaude && info.RelayFormat != types.RelayFormatGemini {
		return nil
	}
	var response dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &response); err != nil {
		return err
	}
	value, _ := c.Get("openai_response_stream_state")
	state, _ := value.(*relayconvert.ResponseStreamState)
	if state == nil {
		var err error
		state, err = relayconvert.NewResponseStreamState(types.RelayFormatOpenAI, info.RelayFormat, relayconvert.ResponseStreamOptions{EmitSequenceNumber: true, ID: response.Id, Model: info.UpstreamModelName, Created: response.Created, MaxInputBytes: helper.ResponseConversionByteLimit()})
		if err != nil {
			return helper.ResponseConversionError(err)
		}
		c.Set("openai_response_stream_state", state)
	}
	results, err := relayconvert.ConvertStreamResponseChunk(c, info, state, &response)
	helper.ObserveResponseStream(c, state)
	if err != nil {
		return helper.ResponseConversionError(err)
	}
	for _, result := range results {
		if err := helper.WriteConvertedStreamResult(c, result); err != nil {
			return err
		}
		if output, ok := result.Value.(*dto.GeminiChatResponse); ok && output != nil && c.Writer.Written() {
			encoded, err := common.Marshal(output)
			if err != nil {
				return err
			}
			if semantic, _ := helper.StreamPayloadState(string(encoded)); semantic {
				info.SetFirstResponseTime()
				info.MarkValidOutput()
			}
		}
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
			return "", io.ErrUnexpectedEOF
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

	case types.RelayFormatClaude, types.RelayFormatGemini:
		if err := HandleStreamFormat(c, info, lastStreamData, false, false); err != nil {
			return err
		}
		value, _ := c.Get("openai_response_stream_state")
		state, ok := value.(*relayconvert.ResponseStreamState)
		if !ok || state == nil {
			return fmt.Errorf("missing response conversion state")
		}
		state.SetUsage(usage)
		if err := helper.FinalizeConvertedStream(c, info, state); err != nil {
			return err
		}
	}
	return nil
}
