package geminichat

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/jsonutil"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

func GeminiGenerateContentRequestToOpenAIChat(geminiRequest *dto.GeminiChatRequest, info convmeta.Meta) (*dto.GeneralOpenAIRequest, error) {
	modelName := ""
	isStream := false
	if info != nil {
		isStream = info.GetIsStream()
	}
	modelName = convmeta.UpstreamModelName(info)
	openaiRequest := &dto.GeneralOpenAIRequest{
		Model:  modelName,
		Stream: kitutil.GetPointer(isStream),
	}

	var messages []dto.Message
	history := make(map[string]string)
	pending := make(map[string]string)
	reserved := make(map[string]bool)
	for _, content := range geminiRequest.Contents {
		for _, part := range content.Parts {
			if part.FunctionCall != nil && part.FunctionCall.ID != "" {
				reserved[part.FunctionCall.ID] = true
			}
		}
	}
	nextCall := 1
	for _, content := range geminiRequest.Contents {
		message := dto.Message{
			Role: convertGeminiRoleToOpenAI(content.Role),
		}

		var mediaContents []dto.MediaContent
		var toolCalls []dto.ToolCallRequest
		var toolResults []dto.Message
		for _, part := range content.Parts {
			if part.Text != "" {
				mediaContent := dto.MediaContent{
					Type: "text",
					Text: part.Text,
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.InlineData != nil {
				if !strings.HasPrefix(strings.ToLower(part.InlineData.MimeType), "image/") {
					return nil, fmt.Errorf("Gemini media cannot be represented as a Chat image")
				}
				imageData, err := normalizeInlineImageBase64(part.InlineData.Data)
				if err != nil {
					return nil, err
				}
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      fmt.Sprintf("data:%s;base64,%s", part.InlineData.MimeType, imageData),
						MimeType: part.InlineData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FileData != nil {
				if !strings.HasPrefix(strings.ToLower(part.FileData.MimeType), "image/") {
					return nil, fmt.Errorf("Gemini media cannot be represented as a Chat image")
				}
				mediaContent := dto.MediaContent{
					Type: "image_url",
					ImageUrl: &dto.MessageImageUrl{
						Url:      part.FileData.FileUri,
						MimeType: part.FileData.MimeType,
					},
				}
				mediaContents = append(mediaContents, mediaContent)
			} else if part.FunctionCall != nil {
				id := strings.TrimSpace(part.FunctionCall.ID)
				if id == "" {
					for {
						id = fmt.Sprintf("call_%d", nextCall)
						nextCall++
						if _, exists := history[id]; !exists && !reserved[id] {
							break
						}
					}
				}
				if _, exists := history[id]; exists {
					return nil, fmt.Errorf("duplicate Gemini function call id %q", id)
				}
				history[id], pending[id] = part.FunctionCall.FunctionName, part.FunctionCall.FunctionName
				toolCall := dto.ToolCallRequest{
					ID:   id,
					Type: "function",
					Function: dto.FunctionRequest{
						Name:      part.FunctionCall.FunctionName,
						Arguments: jsonutil.ToJSONString(part.FunctionCall.Arguments),
					},
				}
				toolCalls = append(toolCalls, toolCall)
			} else if part.FunctionResponse != nil {
				id := ""
				if len(part.FunctionResponse.ID) != 0 {
					if err := kitutil.Unmarshal(part.FunctionResponse.ID, &id); err != nil {
						return nil, fmt.Errorf("Gemini function response id must be a string: %w", err)
					}
				}
				if id == "" {
					for callID, name := range pending {
						if name != part.FunctionResponse.Name {
							continue
						}
						if id != "" {
							return nil, fmt.Errorf("ambiguous Gemini result for function %q requires an id", name)
						}
						id = callID
					}
				}
				name, found := pending[id]
				if !found || name != part.FunctionResponse.Name {
					return nil, fmt.Errorf("Gemini function response %q has no matching outstanding call", part.FunctionResponse.Name)
				}
				delete(pending, id)
				toolMessage := dto.Message{
					Role:       "tool",
					ToolCallId: id,
				}
				toolMessage.SetStringContent(jsonutil.ToJSONString(part.FunctionResponse.Response))
				toolResults = append(toolResults, toolMessage)
			}
		}

		if len(toolCalls) > 0 {
			message.SetToolCalls(toolCalls)
		}
		if len(mediaContents) == 1 && mediaContents[0].Type == "text" {
			message.Content = mediaContents[0].Text
		} else if len(mediaContents) > 0 {
			message.SetMediaContent(mediaContents)
		}

		if len(message.ParseContent()) > 0 || len(message.ToolCalls) > 0 {
			messages = append(messages, message)
		}
		messages = append(messages, toolResults...)
	}

	openaiRequest.Messages = messages

	if geminiRequest.GenerationConfig.Temperature != nil {
		openaiRequest.Temperature = geminiRequest.GenerationConfig.Temperature
	}
	if geminiRequest.GenerationConfig.TopP != nil {
		openaiRequest.TopP = kitutil.GetPointer(*geminiRequest.GenerationConfig.TopP)
	}
	if geminiRequest.GenerationConfig.TopK != nil {
		openaiRequest.TopK = kitutil.GetPointer(int(*geminiRequest.GenerationConfig.TopK))
	}
	if geminiRequest.GenerationConfig.MaxOutputTokens != nil {
		openaiRequest.MaxTokens = kitutil.GetPointer(*geminiRequest.GenerationConfig.MaxOutputTokens)
	}
	if len(geminiRequest.GenerationConfig.StopSequences) > 0 {
		openaiRequest.Stop = geminiRequest.GenerationConfig.StopSequences[:min(len(geminiRequest.GenerationConfig.StopSequences), 4)]
	}
	if geminiRequest.GenerationConfig.CandidateCount != nil {
		openaiRequest.N = kitutil.GetPointer(*geminiRequest.GenerationConfig.CandidateCount)
	}

	if len(geminiRequest.GetTools()) > 0 {
		var tools []dto.ToolCallRequest
		for _, tool := range geminiRequest.GetTools() {
			if tool.FunctionDeclarations == nil {
				continue
			}
			functionDeclarations, err := kitutil.Any2Type[[]dto.FunctionRequest](tool.FunctionDeclarations)
			if err != nil {
				return nil, fmt.Errorf("failed to parse Gemini function declarations: %w", err)
			}
			for _, function := range functionDeclarations {
				openAITool := dto.ToolCallRequest{
					Type: "function",
					Function: dto.FunctionRequest{
						Name:        function.Name,
						Description: function.Description,
						Parameters:  function.Parameters,
					},
				}
				tools = append(tools, openAITool)
			}
		}
		if len(tools) > 0 {
			openaiRequest.Tools = tools
		}
	}
	if geminiRequest.ToolConfig != nil && geminiRequest.ToolConfig.FunctionCallingConfig != nil {
		config := geminiRequest.ToolConfig.FunctionCallingConfig
		switch config.Mode {
		case "AUTO":
			openaiRequest.ToolChoice = "auto"
		case "NONE":
			openaiRequest.ToolChoice = "none"
		case "ANY":
			if len(config.AllowedFunctionNames) == 1 {
				openaiRequest.ToolChoice = map[string]any{"type": "function", "function": map[string]any{"name": config.AllowedFunctionNames[0]}}
			} else {
				openaiRequest.ToolChoice = "required"
				if len(config.AllowedFunctionNames) > 0 {
					allowed := make(map[string]bool, len(config.AllowedFunctionNames))
					for _, name := range config.AllowedFunctionNames {
						allowed[name] = true
					}
					kept := make([]dto.ToolCallRequest, 0, len(allowed))
					for _, tool := range openaiRequest.Tools {
						if allowed[tool.Function.Name] {
							kept = append(kept, tool)
							delete(allowed, tool.Function.Name)
						}
					}
					if len(allowed) != 0 {
						return nil, fmt.Errorf("Gemini tool choice references undefined functions")
					}
					openaiRequest.Tools = kept
				}
			}
		case "":
		default:
			return nil, fmt.Errorf("unsupported Gemini tool choice %q", config.Mode)
		}
	}

	if geminiRequest.SystemInstructions != nil {
		systemMessage := dto.Message{
			Role:    "system",
			Content: extractTextFromGeminiParts(geminiRequest.SystemInstructions.Parts),
		}
		openaiRequest.Messages = append([]dto.Message{systemMessage}, openaiRequest.Messages...)
	}

	return openaiRequest, nil
}

func normalizeInlineImageBase64(value string) (string, error) {
	// Google's Python SDK serializes bytes with the URL-safe alphabet, while
	// an RFC 2397 data URI uses the standard Base64 alphabet.
	value = strings.NewReplacer("-", "+", "_", "/").Replace(value)
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(value)
	}
	if err != nil {
		return "", fmt.Errorf("invalid Gemini inline image encoding: %w", err)
	}
	return base64.StdEncoding.EncodeToString(decoded), nil
}

func convertGeminiRoleToOpenAI(geminiRole string) string {
	switch geminiRole {
	case "user":
		return "user"
	case "model":
		return "assistant"
	case "function":
		return "function"
	default:
		return "user"
	}
}

func extractTextFromGeminiParts(parts []dto.GeminiPart) string {
	texts := make([]string, 0)
	for _, part := range parts {
		if part.Text != "" {
			texts = append(texts, part.Text)
		}
	}
	return strings.Join(texts, "\n")
}
