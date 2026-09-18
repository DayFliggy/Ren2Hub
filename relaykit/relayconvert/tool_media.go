package relayconvert

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Convert the complete ordered history directly. A text-only Chat pivot cannot
// preserve interleaved message/tool blocks, even if images are patched back later.
type toolMediaHistory struct {
	responses []any
	claude    []dto.ClaudeMessage
	system    []any
}

func hasToolMedia(req RequestRequirements) bool {
	for _, media := range req.Media {
		if media.ToolResult {
			return true
		}
	}
	return false
}

func toolMediaLoss(from types.RelayFormat, code, path, message string) error {
	target := types.RelayFormat(types.RelayFormatClaude)
	if from == types.RelayFormatClaude {
		target = types.RelayFormatOpenAIResponses
	}
	return &types.ConversionLossError{Diagnostics: []types.ConversionDiagnostic{{Code: code, Path: path, Message: message,
		Severity: types.ConversionDiagnosticError, From: from, To: target}}}
}

func prepareToolMediaHistory(request any, req RequestRequirements, target types.RelayFormat) (*toolMediaHistory, error) {
	if !hasToolMedia(req) || req.Format == target {
		return nil, nil
	}
	data, err := kitutil.Marshal(request)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := kitutil.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	history, err := convertToolMediaHistory(payload, req.Format)
	if err != nil {
		return nil, err
	}
	// request is the per-attempt clone produced by preparePortableTools. The
	// existing converter handles only its parameter envelope, never its history.
	switch cloned := request.(type) {
	case *dto.ClaudeRequest:
		cloned.Messages = nil
	case *dto.OpenAIResponsesRequest:
		cloned.Input = []byte(`[]`)
	default:
		return nil, fmt.Errorf("unsupported tool media source %T", request)
	}
	return history, nil
}

func (h *toolMediaHistory) apply(request any) error {
	if h == nil {
		return nil
	}
	switch target := request.(type) {
	case *dto.OpenAIResponsesRequest:
		input, err := kitutil.Marshal(h.responses)
		if err != nil {
			return err
		}
		target.Input = input
	case *dto.ClaudeRequest:
		target.Messages = h.claude
		if len(h.system) > 0 {
			var system []any
			if text, ok := target.System.(string); ok {
				system = append(system, map[string]any{"type": "text", "text": text})
			} else if target.System != nil {
				data, err := kitutil.Marshal(target.System)
				if err != nil {
					return err
				}
				if err := kitutil.Unmarshal(data, &system); err != nil {
					return err
				}
			}
			target.System = append(system, h.system...)
		}
	default:
		return fmt.Errorf("unsupported tool media destination %T", request)
	}
	return nil
}

func convertToolMediaHistory(payload map[string]any, from types.RelayFormat) (*toolMediaHistory, error) {
	history := &toolMediaHistory{}
	// One declaration and result may share an ID; repeated declarations or
	// results are ambiguous and must not overwrite one another in a map.
	calls, results := make(map[string]bool), make(map[string]bool)
	if from == types.RelayFormatClaude {
		messages, _ := payload["messages"].([]any)
		for i, raw := range messages {
			message, _ := raw.(map[string]any)
			role := mediaString(message["role"])
			path := fmt.Sprintf("messages[%d].content", i)
			blocks, err := toolHistoryParts(message["content"], from, path)
			if err != nil {
				return nil, err
			}
			for j, rawBlock := range blocks {
				block, _ := rawBlock.(map[string]any)
				blockPath := fmt.Sprintf("%s[%d]", path, j)
				switch block["type"] {
				case "tool_use":
					id := mediaString(block["id"])
					if err := checkToolHistoryIdentity(calls, id, from, blockPath+".id"); err != nil {
						return nil, err
					}
					arguments, err := kitutil.Marshal(block["input"])
					if err != nil {
						return nil, err
					}
					history.responses = append(history.responses, map[string]any{"type": "function_call", "call_id": id, "name": block["name"], "arguments": string(arguments)})
				case "tool_result":
					id := mediaString(block["tool_use_id"])
					if err := checkToolHistoryIdentity(results, id, from, blockPath+".tool_use_id"); err != nil {
						return nil, err
					}
					if failed, _ := block["is_error"].(bool); failed {
						return nil, toolMediaLoss(from, "tool_result_status_not_portable", blockPath+".is_error", "Responses cannot preserve the failed tool execution status")
					}
					output, err := convertToolHistoryContent(block["content"], from, blockPath+".content")
					if err != nil {
						return nil, err
					}
					history.responses = append(history.responses, map[string]any{"type": "function_call_output", "call_id": id, "output": output})
				default:
					parts, err := convertToolMediaParts([]any{block}, from, blockPath)
					if err != nil {
						return nil, err
					}
					if role == "assistant" && parts[0].(map[string]any)["type"] == "input_text" {
						parts[0].(map[string]any)["type"] = "output_text"
					}
					history.responses = append(history.responses, map[string]any{"role": role, "content": parts})
				}
			}
		}
		return history, nil
	}
	items, _ := payload["input"].([]any)
	for i, raw := range items {
		item, _ := raw.(map[string]any)
		path := fmt.Sprintf("input[%d]", i)
		switch item["type"] {
		case "function_call":
			id := mediaString(item["call_id"])
			if err := checkToolHistoryIdentity(calls, id, from, path+".call_id"); err != nil {
				return nil, err
			}
			var arguments map[string]any
			if err := kitutil.Unmarshal([]byte(mediaString(item["arguments"])), &arguments); err != nil || arguments == nil {
				return nil, toolMediaLoss(from, "tool_media_content_unsupported", path+".arguments", "tool arguments must be a JSON object")
			}
			history.appendClaude("assistant", []any{map[string]any{"type": "tool_use", "id": id, "name": item["name"], "input": arguments}})
		case "function_call_output":
			id := mediaString(item["call_id"])
			if err := checkToolHistoryIdentity(results, id, from, path+".call_id"); err != nil {
				return nil, err
			}
			content, err := convertToolHistoryContent(item["output"], from, path+".output")
			if err != nil {
				return nil, err
			}
			history.appendClaude("user", []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": content}})
		case "message", "", nil:
			parts, err := toolHistoryParts(item["content"], from, path+".content")
			if err != nil {
				return nil, err
			}
			mapped, err := convertToolMediaParts(parts, from, path+".content")
			if err != nil {
				return nil, err
			}
			role := mediaString(item["role"])
			if role == "system" || role == "developer" {
				history.system = append(history.system, mapped...)
			} else {
				history.appendClaude(role, mapped)
			}
		default:
			return nil, toolMediaLoss(from, "tool_media_content_unsupported", path+".type", "this history item cannot accompany cross-protocol tool media")
		}
	}
	return history, nil
}

func checkToolHistoryIdentity(seen map[string]bool, id string, from types.RelayFormat, path string) error {
	if strings.TrimSpace(id) == "" {
		return toolMediaLoss(from, "tool_result_identity_missing", path, "tool history requires an explicit call identity")
	}
	if seen[id] {
		return toolMediaLoss(from, "tool_result_identity_ambiguous", path, "repeated tool identities cannot be matched unambiguously")
	}
	seen[id] = true
	return nil
}

func (h *toolMediaHistory) appendClaude(role string, parts []any) {
	if len(h.claude) > 0 && h.claude[len(h.claude)-1].Role == role {
		last := &h.claude[len(h.claude)-1]
		last.Content = append(last.Content.([]any), parts...)
		return
	}
	h.claude = append(h.claude, dto.ClaudeMessage{Role: role, Content: parts})
}

func toolHistoryParts(content any, from types.RelayFormat, path string) ([]any, error) {
	if text, ok := content.(string); ok {
		return []any{map[string]any{"type": "text", "text": text}}, nil
	}
	if parts, ok := content.([]any); ok {
		return parts, nil
	}
	return nil, toolMediaLoss(from, "tool_media_content_unsupported", path, "tool media history requires text or structured content")
}

func convertToolHistoryContent(content any, from types.RelayFormat, path string) (any, error) {
	if text, ok := content.(string); ok {
		return text, nil
	}
	parts, err := toolHistoryParts(content, from, path)
	if err != nil {
		return nil, err
	}
	return convertToolMediaParts(parts, from, path)
}

func convertToolMediaParts(parts []any, from types.RelayFormat, path string) ([]any, error) {
	result := make([]any, 0, len(parts))
	for i, raw := range parts {
		part, _ := raw.(map[string]any)
		partPath := fmt.Sprintf("%s[%d]", path, i)
		switch part["type"] {
		case "text", "input_text", "output_text":
			text, ok := part["text"].(string)
			if !ok {
				return nil, toolMediaLoss(from, "tool_media_content_unsupported", partPath+".text", "text content must be a string")
			}
			kind := "text"
			if from == types.RelayFormatClaude {
				kind = "input_text"
			}
			result = append(result, map[string]any{"type": kind, "text": text})
		case "image":
			if from != types.RelayFormatClaude {
				return nil, toolMediaLoss(from, "tool_media_content_unsupported", partPath+".type", "image content does not match the source protocol")
			}
			source, _ := part["source"].(map[string]any)
			imageURL := mediaString(source["url"])
			if source["type"] == "base64" {
				imageURL = "data:" + mediaString(source["media_type"]) + ";base64," + mediaString(source["data"])
			} else if source["type"] != "url" {
				return nil, toolMediaLoss(from, "tool_media_content_unsupported", partPath+".source", "image content requires a supported source")
			}
			result = append(result, map[string]any{"type": "input_image", "image_url": imageURL})
		case "input_image":
			if from != types.RelayFormatOpenAIResponses {
				return nil, toolMediaLoss(from, "tool_media_content_unsupported", partPath+".type", "image content does not match the source protocol")
			}
			if detail := mediaString(part["detail"]); detail != "" && detail != "auto" {
				return nil, toolMediaLoss(from, "tool_media_detail_not_portable", partPath+".detail", "Claude cannot preserve the requested image detail")
			}
			imageURL := mediaString(part["image_url"])
			source := map[string]any{"type": "url", "url": imageURL}
			if strings.HasPrefix(imageURL, "data:") {
				header, data, found := strings.Cut(strings.TrimPrefix(imageURL, "data:"), ",")
				if !found || !strings.HasSuffix(header, ";base64") || data == "" {
					return nil, toolMediaLoss(from, "tool_media_content_unsupported", partPath+".image_url", "tool image requires a base64 data URL")
				}
				source = map[string]any{"type": "base64", "media_type": strings.TrimSuffix(header, ";base64"), "data": data}
			}
			result = append(result, map[string]any{"type": "image", "source": source})
		default:
			return nil, toolMediaLoss(from, "tool_media_content_unsupported", partPath+".type", "this content cannot accompany cross-protocol tool media")
		}
	}
	return result, nil
}
