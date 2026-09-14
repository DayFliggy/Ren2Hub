package ollama

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

// prepareOllamaMessages resolves tool results against the request's history. It
// does not download media, mutate the source DTO or invent missing arguments.
func prepareOllamaMessages(messages []dto.Message) ([]OllamaChatMessage, [][]dto.MediaContent, error) {
	converted := make([]OllamaChatMessage, len(messages))
	parts := make([][]dto.MediaContent, len(messages))
	calls := make([][]dto.ToolCallRequest, len(messages))
	reserved := make(map[string]bool)
	for index, message := range messages {
		if len(message.ToolCalls) != 0 {
			if err := common.Unmarshal(message.ToolCalls, &calls[index]); err != nil {
				return nil, nil, fmt.Errorf("invalid messages[%d].tool_calls: %w", index, err)
			}
			for _, call := range calls[index] {
				if call.ID != "" {
					if reserved[call.ID] {
						return nil, nil, fmt.Errorf("duplicate tool call id %q", call.ID)
					}
					reserved[call.ID] = true
				}
			}
		}
	}
	pending := make(map[string]string)
	nextID := 0
	for index, message := range messages {
		out := &converted[index]
		out.Role = message.Role
		if message.Role != "tool" && len(pending) != 0 {
			return nil, nil, fmt.Errorf("messages[%d] precedes outstanding tool results", index)
		}
		if text, ok := message.Content.(string); ok {
			out.Content = text
		} else if message.Content != nil {
			encoded, err := common.Marshal(message.Content)
			if err != nil {
				return nil, nil, fmt.Errorf("invalid messages[%d].content: %w", index, err)
			}
			if err := common.Unmarshal(encoded, &parts[index]); err != nil {
				return nil, nil, fmt.Errorf("invalid messages[%d].content: %w", index, err)
			}
			var text strings.Builder
			for partIndex, part := range parts[index] {
				switch part.Type {
				case dto.ContentTypeText:
					text.WriteString(part.Text)
				case dto.ContentTypeImageURL:
					if part.ToFileSource() == nil {
						return nil, nil, fmt.Errorf("messages[%d].content[%d] has no image source", index, partIndex)
					}
				default:
					return nil, nil, fmt.Errorf("ollama cannot represent messages[%d].content[%d] type %q", index, partIndex, part.Type)
				}
			}
			out.Content = text.String()
		}
		if message.Role == "assistant" && (message.ReasoningContent != nil || message.Reasoning != nil) {
			thinking, err := common.Marshal(message.GetReasoningContent())
			if err != nil {
				return nil, nil, err
			}
			out.Thinking = thinking
		}
		if message.Role == "tool" {
			id, name := message.ToolCallId, ""
			if message.Name != nil {
				name = *message.Name
			}
			if id == "" {
				for candidate, callName := range pending {
					if name != "" && name != callName {
						continue
					}
					if id != "" {
						return nil, nil, fmt.Errorf("ambiguous messages[%d] tool result requires a tool_call_id", index)
					}
					id = candidate
				}
			}
			callName, found := pending[id]
			if !found || (name != "" && name != callName) {
				return nil, nil, fmt.Errorf("messages[%d] tool result has no matching outstanding call", index)
			}
			out.ToolCallID, out.ToolName = id, callName
			delete(pending, id)
		}
		if len(calls[index]) != 0 && message.Role != "assistant" {
			return nil, nil, fmt.Errorf("messages[%d].tool_calls requires the assistant role", index)
		}
		for callIndex, call := range calls[index] {
			if call.Type != "function" || strings.TrimSpace(call.Function.Name) == "" {
				return nil, nil, fmt.Errorf("messages[%d].tool_calls[%d] must name a function", index, callIndex)
			}
			var arguments map[string]json.RawMessage
			if err := common.Unmarshal([]byte(call.Function.Arguments), &arguments); err != nil || arguments == nil {
				return nil, nil, fmt.Errorf("messages[%d].tool_calls[%d].function.arguments must be a JSON object", index, callIndex)
			}
			id := call.ID
			if id == "" {
				for {
					id = fmt.Sprintf("call_ollama_%d", nextID)
					nextID++
					if !reserved[id] {
						reserved[id] = true
						break
					}
				}
			}
			convertedCall := OllamaToolCall{ID: id}
			convertedCall.Function.Name, convertedCall.Function.Arguments = call.Function.Name, json.RawMessage(call.Function.Arguments)
			out.ToolCalls = append(out.ToolCalls, convertedCall)
			pending[id] = call.Function.Name
		}
	}
	if len(pending) != 0 {
		return nil, nil, fmt.Errorf("ollama request is missing tool results")
	}
	return converted, parts, nil
}
