package relayconvert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Tool media travels alongside the text pivot; serializing it as tool text
// would preserve bytes but remove the model's visual input.
func preserveToolMedia(original, converted any, from, to types.RelayFormat) error {
	if !(from == types.RelayFormatClaude && to == types.RelayFormatOpenAIResponses || from == types.RelayFormatOpenAIResponses && to == types.RelayFormatClaude) {
		return nil
	}
	data, err := kitutil.Marshal(original)
	if err != nil {
		return err
	}
	var source map[string]any
	if err := kitutil.Unmarshal(data, &source); err != nil {
		return err
	}
	outputs := make(map[string][]any)
	if from == types.RelayFormatClaude {
		messages, _ := source["messages"].([]any)
		for _, raw := range messages {
			message, _ := raw.(map[string]any)
			blocks, _ := message["content"].([]any)
			for _, rawBlock := range blocks {
				block, _ := rawBlock.(map[string]any)
				if block["type"] != "tool_result" {
					continue
				}
				parts, _ := block["content"].([]any)
				mapped, hasImage, err := convertToolMediaParts(parts, from)
				if err != nil {
					return err
				}
				if hasImage {
					outputs[mediaString(block["tool_use_id"])] = mapped
				}
			}
		}
	} else {
		items, _ := source["input"].([]any)
		for _, raw := range items {
			item, _ := raw.(map[string]any)
			if item["type"] != "function_call_output" {
				continue
			}
			parts, _ := item["output"].([]any)
			mapped, hasImage, err := convertToolMediaParts(parts, from)
			if err != nil {
				return err
			}
			if hasImage {
				outputs[mediaString(item["call_id"])] = mapped
			}
		}
	}
	if len(outputs) == 0 {
		return nil
	}
	if _, emptyID := outputs[""]; emptyID {
		return fmt.Errorf("tool media requires a call identity")
	}
	switch target := converted.(type) {
	case *dto.OpenAIResponsesRequest:
		var items []map[string]json.RawMessage
		if err := kitutil.Unmarshal(target.Input, &items); err != nil {
			return err
		}
		for _, item := range items {
			var id, kind string
			_ = kitutil.Unmarshal(item["call_id"], &id)
			_ = kitutil.Unmarshal(item["type"], &kind)
			if parts, found := outputs[id]; found && kind == "function_call_output" {
				encoded, err := kitutil.Marshal(parts)
				if err != nil {
					return err
				}
				item["output"] = encoded
				delete(outputs, id)
			}
		}
		if len(outputs) != 0 {
			return fmt.Errorf("converted request lost a tool media call identity")
		}
		target.Input, err = kitutil.Marshal(items)
		return err
	case *dto.ClaudeRequest:
		for i := range target.Messages {
			message := &target.Messages[i]
			if message.IsStringContent() {
				continue
			}
			encoded, err := kitutil.Marshal(message.Content)
			if err != nil {
				return err
			}
			var blocks []map[string]json.RawMessage
			if err := kitutil.Unmarshal(encoded, &blocks); err != nil {
				return err
			}
			changed := false
			for _, block := range blocks {
				var id, kind string
				_ = kitutil.Unmarshal(block["tool_use_id"], &id)
				_ = kitutil.Unmarshal(block["type"], &kind)
				if parts, found := outputs[id]; found && kind == "tool_result" {
					encoded, err := kitutil.Marshal(parts)
					if err != nil {
						return err
					}
					block["content"] = encoded
					delete(outputs, id)
					changed = true
				}
			}
			if changed {
				message.Content = blocks
			}
		}
		if len(outputs) != 0 {
			return fmt.Errorf("converted request lost a tool media call identity")
		}
		return nil
	default:
		return fmt.Errorf("unexpected tool media destination %T", converted)
	}
}

func convertToolMediaParts(parts []any, from types.RelayFormat) ([]any, bool, error) {
	hasImage := false
	for _, raw := range parts {
		part, _ := raw.(map[string]any)
		hasImage = hasImage || part["type"] == "image" || part["type"] == "input_image"
	}
	if !hasImage {
		return nil, false, nil
	}
	result := make([]any, 0, len(parts))
	for _, raw := range parts {
		part, _ := raw.(map[string]any)
		switch part["type"] {
		case "text", "input_text":
			kind := "text"
			if from == types.RelayFormatClaude {
				kind = "input_text"
			}
			result = append(result, map[string]any{"type": kind, "text": part["text"]})
		case "image":
			source, _ := part["source"].(map[string]any)
			imageURL := mediaString(source["url"])
			if source["type"] == "base64" {
				imageURL = "data:" + mediaString(source["media_type"]) + ";base64," + mediaString(source["data"])
			}
			result = append(result, map[string]any{"type": "input_image", "image_url": imageURL})
		case "input_image":
			imageURL := mediaString(part["image_url"])
			source := map[string]any{"type": "url", "url": imageURL}
			if strings.HasPrefix(imageURL, "data:") {
				header, data, found := strings.Cut(strings.TrimPrefix(imageURL, "data:"), ",")
				if !found || !strings.HasSuffix(header, ";base64") {
					return nil, false, fmt.Errorf("tool image requires a base64 data URL")
				}
				source = map[string]any{"type": "base64", "media_type": strings.TrimSuffix(header, ";base64"), "data": data}
			}
			result = append(result, map[string]any{"type": "image", "source": source})
		default:
			return nil, false, fmt.Errorf("unsupported content in a multimodal tool result")
		}
	}
	return result, true, nil
}
