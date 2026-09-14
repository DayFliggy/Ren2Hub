package relayconvert

import (
	"fmt"
	"reflect"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// Clone before removing nonportable definitions. Original DTOs remain available
// for retries and native forwarding; rejected losses were checked by the plan.
func preparePortableTools(request any, target types.RelayFormat) (any, error) {
	data, err := kitutil.Marshal(request)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := kitutil.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	format, err := inferRequestRelayFormat(request)
	if err != nil {
		return nil, err
	}
	if tools, ok := payload["tools"].([]any); ok {
		kept := make([]any, 0, len(tools))
		for _, value := range tools {
			tool, ok := value.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("tool must be an object")
			}
			if format == types.RelayFormatGemini {
				if functions, ok := tool["functionDeclarations"]; ok {
					kept = append(kept, map[string]any{"functionDeclarations": functions})
				}
				continue
			}
			kind, _ := tool["type"].(string)
			if kind == "function" || (kind == "" && format == types.RelayFormatClaude) {
				if target == types.RelayFormatGemini {
					delete(tool, "strict")
					if function, ok := tool["function"].(map[string]any); ok {
						delete(function, "strict")
					}
				}
				kept = append(kept, tool)
			}
		}
		payload["tools"] = kept
	}
	if format != types.RelayFormatOpenAI || target != types.RelayFormatClaude {
		delete(payload, "web_search_options")
	}
	data, err = kitutil.Marshal(payload)
	if err != nil {
		return nil, err
	}
	valueType := reflect.TypeOf(request)
	if valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	cloned := reflect.New(valueType).Interface()
	if err := kitutil.Unmarshal(data, cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func attachPortableSearch(request any, requirements RequestRequirements) error {
	search := false
	for _, tool := range requirements.Tools {
		search = search || tool.Type == "web_search"
	}
	if !search {
		return nil
	}
	switch req := request.(type) {
	case *dto.GeneralOpenAIRequest:
		req.WebSearchOptions = &dto.WebSearchOptions{}
	case *dto.GeminiChatRequest:
		tools := req.GetTools()
		tools = append(tools, dto.GeminiChatTool{GoogleSearch: map[string]any{}})
		req.SetTools(tools)
	case *dto.OpenAIResponsesRequest:
		var tools []map[string]any
		if len(req.Tools) != 0 {
			if err := kitutil.Unmarshal(req.Tools, &tools); err != nil {
				return err
			}
		}
		tools = append(tools, map[string]any{"type": "web_search"})
		encoded, err := kitutil.Marshal(tools)
		if err != nil {
			return err
		}
		req.Tools = encoded
	case *dto.ClaudeRequest:
		tools, err := kitutil.Any2Type[[]any](req.Tools)
		if err != nil && req.Tools != nil {
			return err
		}
		for _, value := range tools {
			tool, ok := value.(map[string]any)
			if ok && tool["name"] == "web_search" {
				return nil
			}
		}
		req.Tools = append(tools, map[string]any{"type": "web_search_20250305", "name": "web_search"})
	default:
		return fmt.Errorf("cannot attach search to %T", request)
	}
	return nil
}
