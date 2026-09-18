package geminichat

import (
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func UsageFromGeminiMetadata(metadata *dto.GeminiUsageMetadata, fallbackPromptTokens int) *dto.Usage {
	if metadata == nil {
		if fallbackPromptTokens <= 0 {
			return nil
		}
		usage := &dto.Usage{PromptTokens: fallbackPromptTokens}
		usage.PromptTokensDetails.TextTokens = fallbackPromptTokens
		return usage
	}

	promptTokens := metadata.PromptTokenCount + metadata.ToolUsePromptTokenCount
	if promptTokens <= 0 && fallbackPromptTokens > 0 {
		promptTokens = fallbackPromptTokens
	}

	usage := &dto.Usage{
		PromptTokens:     promptTokens,
		CompletionTokens: metadata.CandidatesTokenCount + metadata.ThoughtsTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
		BillingUsage:     dto.CloneBillingUsage(metadata.BillingUsage),
	}
	if usage.BillingUsage == nil {
		usage.BillingUsage = dto.NewGeminiChatBillingUsage(metadata)
	}
	usage.CompletionTokenDetails.ReasoningTokens = metadata.ThoughtsTokenCount
	usage.PromptTokensDetails.CachedTokens = metadata.CachedContentTokenCount

	for _, detail := range metadata.PromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "IMAGE" {
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.ToolUsePromptTokensDetails {
		if detail.Modality == "AUDIO" {
			usage.PromptTokensDetails.AudioTokens += detail.TokenCount
		} else if detail.Modality == "IMAGE" {
			usage.PromptTokensDetails.ImageTokens += detail.TokenCount
		} else if detail.Modality == "TEXT" {
			usage.PromptTokensDetails.TextTokens += detail.TokenCount
		}
	}
	for _, detail := range metadata.CandidatesTokensDetails {
		switch detail.Modality {
		case "IMAGE":
			usage.CompletionTokenDetails.ImageTokens += detail.TokenCount
		case "AUDIO":
			usage.CompletionTokenDetails.AudioTokens += detail.TokenCount
		case "TEXT":
			usage.CompletionTokenDetails.TextTokens += detail.TokenCount
		}
	}

	if usage.TotalTokens > 0 && usage.CompletionTokens <= 0 {
		usage.CompletionTokens = usage.TotalTokens - usage.PromptTokens
	}

	if usage.PromptTokens > 0 && usage.PromptTokensDetails.TextTokens == 0 && usage.PromptTokensDetails.AudioTokens == 0 {
		usage.PromptTokensDetails.TextTokens = usage.PromptTokens
	}

	return usage
}

func ResponseGeminiChat2OpenAI(id string, created int64, response *dto.GeminiChatResponse) *dto.OpenAITextResponse {
	fullTextResponse := dto.OpenAITextResponse{
		Id:      id,
		Object:  "chat.completion",
		Created: created,
		Choices: make([]dto.OpenAITextResponseChoice, 0, len(response.Candidates)),
	}
	for _, candidate := range response.Candidates {
		isToolCall := false
		choice := dto.OpenAITextResponseChoice{
			Index: int(candidate.Index),
			Message: dto.Message{
				Role:    "assistant",
				Content: "",
			},
			FinishReason: types.FinishReasonStop,
		}
		if len(candidate.Content.Parts) > 0 {
			var content strings.Builder
			var inlineGrow int
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
				}
			}
			if inlineGrow > 0 {
				content.Grow(inlineGrow)
			}
			appended := 0
			writeSep := func() {
				if appended > 0 {
					content.WriteByte('\n')
				}
				appended++
			}
			var toolCalls []dto.ToolCallResponse
			for _, part := range candidate.Content.Parts {
				if part.InlineData != nil {
					if strings.HasPrefix(part.InlineData.MimeType, "image") {
						writeSep()
						content.WriteString("![image](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					} else {
						writeSep()
						content.WriteString("[media](data:")
						content.WriteString(part.InlineData.MimeType)
						content.WriteString(";base64,")
						content.WriteString(part.InlineData.Data)
						content.WriteByte(')')
					}
				} else if part.FunctionCall != nil {
					choice.FinishReason = types.FinishReasonToolCalls
					if call := geminiResponseToolCall(&part); call != nil {
						toolCalls = append(toolCalls, *call)
					}
				} else if part.Thought {
					continue
				} else {
					if part.ExecutableCode != nil {
						writeSep()
						content.WriteString("```")
						content.WriteString(part.ExecutableCode.Language)
						content.WriteByte('\n')
						content.WriteString(part.ExecutableCode.Code)
						content.WriteString("\n```")
					} else if part.CodeExecutionResult != nil {
						writeSep()
						content.WriteString("```output\n")
						content.WriteString(part.CodeExecutionResult.Output)
						content.WriteString("\n```")
					} else if part.Text != "\n" {
						writeSep()
						content.WriteString(part.Text)
					}
				}
			}
			if len(toolCalls) > 0 {
				choice.Message.SetToolCalls(toolCalls)
				isToolCall = true
			}
			choice.Message.SetStringContent(content.String())
			if reasoning := geminiThoughtText(candidate.Content.Parts); reasoning != "" {
				choice.Message.ReasoningContent = &reasoning
			}
		}
		if candidate.FinishReason != nil {
			switch *candidate.FinishReason {
			case "STOP":
				choice.FinishReason = types.FinishReasonStop
			case "MAX_TOKENS":
				choice.FinishReason = types.FinishReasonLength
			case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "OTHER":
				choice.FinishReason = types.FinishReasonContentFilter
			default:
				choice.FinishReason = types.FinishReasonContentFilter
			}
		}
		if isToolCall && choice.FinishReason == types.FinishReasonStop {
			choice.FinishReason = types.FinishReasonToolCalls
		}

		choice.Message.Annotations = groundingAnnotationsToChat(candidate.GroundingMetadata, candidate.Content, choice.Message.StringContent())
		fullTextResponse.Choices = append(fullTextResponse.Choices, choice)
	}
	return &fullTextResponse
}

func StreamResponseGeminiChat2OpenAI(geminiResponse *dto.GeminiChatResponse) (*dto.ChatCompletionsStreamResponse, bool) {
	choices := make([]dto.ChatCompletionsStreamResponseChoice, 0, len(geminiResponse.Candidates))
	isStop := false
	for _, candidate := range geminiResponse.Candidates {
		if candidate.FinishReason != nil && *candidate.FinishReason == "STOP" {
			isStop = true
			candidate.FinishReason = nil
		}
		choice := dto.ChatCompletionsStreamResponseChoice{
			Index: int(candidate.Index),
			Delta: dto.ChatCompletionsStreamResponseChoiceDelta{},
		}
		var content strings.Builder
		var inlineGrow int
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				inlineGrow += len(part.InlineData.MimeType) + len(part.InlineData.Data) + 32
			}
		}
		if inlineGrow > 0 {
			content.Grow(inlineGrow)
		}
		appended := 0
		writeSep := func() {
			if appended > 0 {
				content.WriteByte('\n')
			}
			appended++
		}
		isTools := false
		if candidate.FinishReason != nil {
			switch *candidate.FinishReason {
			case "STOP":
				choice.FinishReason = &types.FinishReasonStop
			case "MAX_TOKENS":
				choice.FinishReason = &types.FinishReasonLength
			case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "OTHER":
				choice.FinishReason = &types.FinishReasonContentFilter
			default:
				choice.FinishReason = &types.FinishReasonContentFilter
			}
		}
		for _, part := range candidate.Content.Parts {
			if part.InlineData != nil {
				if strings.HasPrefix(part.InlineData.MimeType, "image") {
					writeSep()
					content.WriteString("![image](data:")
					content.WriteString(part.InlineData.MimeType)
					content.WriteString(";base64,")
					content.WriteString(part.InlineData.Data)
					content.WriteByte(')')
				}
			} else if part.FunctionCall != nil {
				isTools = true
				if call := geminiResponseToolCall(&part); call != nil {
					call.SetIndex(len(choice.Delta.ToolCalls))
					choice.Delta.ToolCalls = append(choice.Delta.ToolCalls, *call)
				}
			} else if part.Thought {
				continue
			} else {
				if part.ExecutableCode != nil {
					writeSep()
					content.WriteString("```")
					content.WriteString(part.ExecutableCode.Language)
					content.WriteByte('\n')
					content.WriteString(part.ExecutableCode.Code)
					content.WriteString("\n```\n")
				} else if part.CodeExecutionResult != nil {
					writeSep()
					content.WriteString("```output\n")
					content.WriteString(part.CodeExecutionResult.Output)
					content.WriteString("\n```\n")
				} else if part.Text != "\n" {
					writeSep()
					content.WriteString(part.Text)
				}
			}
		}
		reasoning := geminiThoughtText(candidate.Content.Parts)
		if reasoning != "" {
			choice.Delta.SetReasoningContent(reasoning)
		}
		if content.Len() > 0 || reasoning == "" {
			choice.Delta.SetContentString(content.String())
		}
		if isTools && choice.FinishReason == nil {
			choice.FinishReason = &types.FinishReasonToolCalls
		}
		choices = append(choices, choice)
	}

	response := dto.ChatCompletionsStreamResponse{
		Object:  "chat.completion.chunk",
		Choices: choices,
	}
	return &response, isStop
}

func geminiThoughtText(parts []dto.GeminiPart) string {
	var text strings.Builder
	for _, part := range parts {
		if !part.Thought || part.Text == "" {
			continue
		}
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(part.Text)
	}
	return text.String()
}

type GeminiToChatStreamState struct {
	id          string
	created     int64
	choices     map[int]*geminiToChatChoiceState
	latestUsage *dto.Usage
}

type geminiToChatChoiceState struct {
	grounding       *geminiGroundingStreamCandidate
	seenAnnotations map[string]struct{}
	sawToolCall     bool
	finishEmitted   bool
	nextToolIndex   int
}

func NewGeminiToChatStreamState(id string, created int64) *GeminiToChatStreamState {
	id = strings.TrimSpace(id)
	if id == "" {
		id = fmt.Sprintf("chatcmpl-%s", kitutil.GetUUID())
	}
	if created == 0 {
		created = kitutil.GetTimestamp()
	}
	return &GeminiToChatStreamState{id: id, created: created, choices: make(map[int]*geminiToChatChoiceState)}
}

func (s *GeminiToChatStreamState) ConvertChunk(geminiResponse *dto.GeminiChatResponse, model string, usage *dto.Usage) []*dto.ChatCompletionsStreamResponse {
	if s == nil || geminiResponse == nil {
		return nil
	}
	if s.choices == nil {
		s.choices = make(map[int]*geminiToChatChoiceState)
	}
	response, _ := StreamResponseGeminiChat2OpenAI(geminiResponse)
	if response == nil {
		return nil
	}
	response.Id = s.id
	response.Created = s.created
	response.Model = model
	response.Usage = usage

	if usage != nil {
		s.latestUsage = usage
	}
	responses := []*dto.ChatCompletionsStreamResponse{response}
	for i := range response.Choices {
		choice := &response.Choices[i]
		state := s.choices[choice.Index]
		if state == nil {
			state = &geminiToChatChoiceState{grounding: newGeminiGroundingStreamCandidate(), seenAnnotations: make(map[string]struct{})}
			s.choices[choice.Index] = state
			choice.Delta.Role = "assistant"
		}
		state.grounding.appendContent(geminiResponse.Candidates[i].Content, choice.Delta.GetContentString())
		choice.Delta.Annotations = state.grounding.groundingAnnotations(geminiResponse.Candidates[i].GroundingMetadata, int64(choice.Index), state.seenAnnotations)
		finish := ""
		if geminiResponse.Candidates[i].FinishReason != nil {
			finish = *geminiResponse.Candidates[i].FinishReason
		}
		if len(choice.Delta.ToolCalls) > 0 {
			state.sawToolCall = true
			for j := range choice.Delta.ToolCalls {
				choice.Delta.ToolCalls[j].SetIndex(state.nextToolIndex)
				state.nextToolIndex++
			}
			if finish == "" || finish == "STOP" {
				choice.FinishReason = nil
			}
		}
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			state.finishEmitted = true
		}
		if finish == "STOP" && !state.finishEmitted {
			responses = append(responses, s.terminalChunk(model, choice.Index))
		}
	}
	return responses
}

func (s *GeminiToChatStreamState) Finalize(model string) []*dto.ChatCompletionsStreamResponse {
	if s == nil {
		return nil
	}
	if len(s.choices) == 0 {
		s.choices = map[int]*geminiToChatChoiceState{0: {}}
	}
	indexes := make([]int, 0, len(s.choices))
	for index, choice := range s.choices {
		if !choice.finishEmitted {
			indexes = append(indexes, index)
		}
	}
	slices.Sort(indexes)
	responses := make([]*dto.ChatCompletionsStreamResponse, 0, len(indexes))
	for _, index := range indexes {
		responses = append(responses, s.terminalChunk(model, index))
	}
	return responses
}

func (s *GeminiToChatStreamState) Usage() *dto.Usage {
	if s == nil {
		return nil
	}
	return s.latestUsage
}

func (s *GeminiToChatStreamState) terminalChunk(model string, index int) *dto.ChatCompletionsStreamResponse {
	state := s.choices[index]
	finishReason := types.FinishReasonStop
	if state.sawToolCall {
		finishReason = types.FinishReasonToolCalls
	}
	state.finishEmitted = true
	return &dto.ChatCompletionsStreamResponse{
		Id:      s.id,
		Object:  "chat.completion.chunk",
		Created: s.created,
		Model:   model,
		Choices: []dto.ChatCompletionsStreamResponseChoice{
			{
				Index:        index,
				Delta:        dto.ChatCompletionsStreamResponseChoiceDelta{},
				FinishReason: &finishReason,
			},
		},
		Usage: s.latestUsage,
	}
}

func geminiResponseToolCall(item *dto.GeminiPart) *dto.ToolCallResponse {
	argsBytes, err := kitutil.Marshal(item.FunctionCall.Arguments)
	if err != nil {
		return nil
	}
	id := item.FunctionCall.ID
	if id == "" {
		id = fmt.Sprintf("call_%s", kitutil.GetUUID())
	}
	return &dto.ToolCallResponse{
		ID:   id,
		Type: "function",
		Function: dto.FunctionResponse{
			Arguments: string(argsBytes),
			Name:      item.FunctionCall.FunctionName,
		},
	}
}

func (s *GeminiToChatStreamState) Error() error {
	if s == nil {
		return nil
	}
	for _, choice := range s.choices {
		if choice.grounding != nil && choice.grounding.err != nil {
			return choice.grounding.err
		}
	}
	return nil
}
