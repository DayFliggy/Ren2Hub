package relayconvert

import (
	"context"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func ClaudeMessagesRequestToOpenAIChat(claudeRequest dto.ClaudeRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	result, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, &claudeRequest)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeneralOpenAIRequest), nil
}

func OpenAIChatRequestToClaudeMessages(c context.Context, info convmeta.Meta, textRequest dto.GeneralOpenAIRequest) (*dto.ClaudeRequest, error) {
	result, err := ConvertRequest(c, info, types.RelayFormatClaude, &textRequest)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.ClaudeRequest), nil
}

func GeminiGenerateContentRequestToOpenAIChat(geminiRequest *dto.GeminiChatRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	result, err := ConvertRequest(context.Background(), info, types.RelayFormatOpenAI, geminiRequest)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeneralOpenAIRequest), nil
}

func OpenAIChatRequestToGeminiGenerateContent(c context.Context, textRequest dto.GeneralOpenAIRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	result, err := ConvertRequest(c, info, types.RelayFormatGemini, &textRequest)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeminiChatRequest), nil
}

func ApplyGeminiThinkingConfig(geminiRequest *dto.GeminiChatRequest, info convmeta.Meta, oaiRequest ...dto.GeneralOpenAIRequest) {
	sharedgemini.ApplyThinkingConfig(geminiRequest, info, oaiRequest...)
}

func ChatCompletionsRequestToResponsesRequest(req *dto.GeneralOpenAIRequest) (*dto.OpenAIResponsesRequest, error) {
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAIResponses, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.OpenAIResponsesRequest), nil
}

func ResponsesRequestToChatCompletionsRequest(req *dto.OpenAIResponsesRequest) (*dto.GeneralOpenAIRequest, error) {
	result, err := ConvertRequest(context.Background(), nil, types.RelayFormatOpenAI, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeneralOpenAIRequest), nil
}

func OpenAIResponsesRequestToClaudeMessages(c context.Context, info convmeta.Meta, req *dto.OpenAIResponsesRequest) (*dto.ClaudeRequest, error) {
	result, err := ConvertRequest(c, info, types.RelayFormatClaude, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.ClaudeRequest), nil
}

func OpenAIResponsesRequestToGeminiChat(c context.Context, req *dto.OpenAIResponsesRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	result, err := ConvertRequest(c, info, types.RelayFormatGemini, req)
	if err != nil {
		return nil, err
	}
	return result.Value.(*dto.GeminiChatRequest), nil
}
