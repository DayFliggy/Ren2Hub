package common

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
)

// ApplyChannelSystemPrompt runs on an attempt's copied request before conversion.
// It never edits the logical request, so retries cannot accumulate the prefix.
func ApplyChannelSystemPrompt(c *gin.Context, info *RelayInfo, request any) error {
	if info == nil || info.ChannelMeta == nil || info.ChannelSetting.SystemPrompt == "" {
		return nil
	}
	data, err := common.Marshal(request)
	if err != nil {
		return err
	}
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(data, &payload); err != nil {
		return err
	}
	if err := applyChannelSystemPromptJSON(c, info, request, payload); err != nil {
		return err
	}
	data, err = common.Marshal(payload)
	if err != nil {
		return err
	}
	if chat, ok := request.(*dto.GeneralOpenAIRequest); ok {
		// Inserting a message shifts the slice. Decoding into its old elements
		// would retain fields omitted by the replacement message, such as tools.
		updated := *chat
		updated.Messages = nil
		if err := common.Unmarshal(data, &updated); err != nil {
			return err
		}
		*chat = updated
		return nil
	}
	return common.Unmarshal(data, request)
}

// Both DTO forwarding and native passthrough use this operation. Editing only
// the instruction content preserves extensions on native messages and parts.
func applyChannelSystemPromptJSON(c *gin.Context, info *RelayInfo, request any, payload map[string]json.RawMessage) error {
	if info.ChannelSetting.SystemPrompt == "" {
		return nil
	}
	prompt, override := info.ChannelSetting.SystemPrompt, info.ChannelSetting.SystemPromptOverride
	var merged bool
	switch request := request.(type) {
	case *dto.GeneralOpenAIRequest:
		var messages []map[string]json.RawMessage
		if err := common.Unmarshal(payload["messages"], &messages); err != nil && len(payload["messages"]) > 0 {
			return fmt.Errorf("messages must be an array: %w", err)
		}
		role := request.GetSystemRoleName()
		index := -1
		for i, message := range messages {
			// Kimi tool-loading messages declare tools, not instructions, and
			// must never receive a content field during prompt injection.
			if len(message["tools"]) > 0 {
				continue
			}
			var existingRole string
			if common.Unmarshal(message["role"], &existingRole) == nil && existingRole == role {
				index = i
				break
			}
		}
		if index < 0 {
			roleJSON, err := common.Marshal(role)
			if err != nil {
				return err
			}
			text, err := common.Marshal(prompt)
			if err != nil {
				return err
			}
			messages = append([]map[string]json.RawMessage{{"role": roleJSON, "content": text}}, messages...)
		} else {
			if !override {
				return nil
			}
			content, err := prefixSystemContent(messages[index]["content"], prompt, true)
			if err != nil {
				return err
			}
			messages[index]["content"], merged = content, true
		}
		data, err := common.Marshal(messages)
		if err != nil {
			return err
		}
		payload["messages"] = data
	case *dto.ClaudeRequest, *dto.OpenAIResponsesRequest:
		field, allowParts := "system", true
		if _, responses := request.(*dto.OpenAIResponsesRequest); responses {
			field, allowParts = "instructions", false
		}
		present := systemContentPresent(payload[field])
		if present && !override {
			return nil
		}
		content, err := prefixSystemContent(payload[field], prompt, allowParts)
		if err != nil {
			return fmt.Errorf("%s: %w", field, err)
		}
		payload[field], merged = content, present
	case *dto.GeminiChatRequest:
		var system map[string]json.RawMessage
		if systemContentPresent(payload["systemInstruction"]) {
			if err := common.Unmarshal(payload["systemInstruction"], &system); err != nil {
				return err
			}
		}
		if system == nil {
			system = make(map[string]json.RawMessage)
		}
		var parts []map[string]json.RawMessage
		if raw := system["parts"]; systemContentPresent(raw) {
			if err := common.Unmarshal(raw, &parts); err != nil {
				return err
			}
		}
		if len(parts) > 0 && !override {
			return nil
		}
		merged = len(parts) > 0
		added := false
		for _, part := range parts {
			var existing string
			if common.Unmarshal(part["text"], &existing) == nil && existing != "" {
				data, err := common.Marshal(prompt + "\n" + existing)
				if err != nil {
					return err
				}
				part["text"], added = data, true
				break
			}
		}
		if !added {
			data, err := common.Marshal(prompt)
			if err != nil {
				return err
			}
			parts = append([]map[string]json.RawMessage{{"text": data}}, parts...)
		}
		data, err := common.Marshal(parts)
		if err != nil {
			return err
		}
		system["parts"] = data
		data, err = common.Marshal(system)
		if err != nil {
			return err
		}
		payload["systemInstruction"] = data
	default:
		return fmt.Errorf("channel system prompt is not supported for %T", request)
	}
	if merged && c != nil {
		common.SetContextKey(c, constant.ContextKeySystemPromptOverride, true)
	}
	return nil
}

func systemContentPresent(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && !bytes.Equal(raw, []byte("null"))
}

func prefixSystemContent(raw json.RawMessage, prompt string, allowParts bool) (json.RawMessage, error) {
	if !systemContentPresent(raw) {
		return common.Marshal(prompt)
	}
	var existing string
	if err := common.Unmarshal(raw, &existing); err == nil {
		if existing != "" {
			prompt += "\n" + existing
		}
		return common.Marshal(prompt)
	}
	if !allowParts {
		return nil, fmt.Errorf("instructions must be a string or null")
	}
	var parts []json.RawMessage
	if err := common.Unmarshal(raw, &parts); err != nil {
		return nil, fmt.Errorf("system content must be a string or array: %w", err)
	}
	part, err := common.Marshal(map[string]string{"type": "text", "text": prompt})
	if err != nil {
		return nil, err
	}
	return common.Marshal(append([]json.RawMessage{part}, parts...))
}
