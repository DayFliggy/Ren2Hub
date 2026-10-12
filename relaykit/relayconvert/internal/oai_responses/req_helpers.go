package oairesponses

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

func openAIResponsesRequestFromAny(request any) (*dto.OpenAIResponsesRequest, error) {
	responsesRequest, ok := request.(*dto.OpenAIResponsesRequest)
	if !ok {
		if value, ok := request.(dto.OpenAIResponsesRequest); ok {
			responsesRequest = &value
		}
	}
	if responsesRequest == nil {
		return nil, fmt.Errorf("expected OpenAI responses request, got %T", request)
	}
	return responsesRequest, nil
}

func OpenAIResponsesRequestFromAny(request any) (*dto.OpenAIResponsesRequest, error) {
	return openAIResponsesRequestFromAny(request)
}

func responsesInputItems(raw []byte) ([]map[string]any, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}

	switch kitutil.GetJsonType(raw) {
	case "string":
		input, err := responsesJSONString(raw)
		if err != nil {
			return nil, fmt.Errorf("invalid input string: %w", err)
		}
		return []map[string]any{
			{
				"role":    "user",
				"content": input,
			},
		}, nil
	case "array":
		var items []map[string]any
		if err := kitutil.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("invalid input array: %w", err)
		}
		return keepToolOutputsAdjacent(items), nil
	default:
		return nil, fmt.Errorf("unsupported responses input type %q", kitutil.GetJsonType(raw))
	}
}

// InputItems decodes Responses input in an order that turn-based protocols
// accept: each tool output follows the run of calls that requested it.
func InputItems(raw []byte) ([]map[string]any, error) {
	return responsesInputItems(raw)
}

// keepToolOutputsAdjacent moves each tool output up to the run of calls that
// requested it; reasoning items interleaved with the calls stay in the run.
// Responses lets a client put a message between a call and its
// output; Chat, Claude and Gemini reject a tool result that does not directly
// follow its call. An output for a later call stays where the client put it.
func keepToolOutputsAdjacent(items []map[string]any) []map[string]any {
	ordered := make([]map[string]any, 0, len(items))
	moved := make([]bool, len(items))
	for i := 0; i < len(items); {
		if moved[i] {
			i++
			continue
		}
		if !isResponsesToolCall(items[i]) {
			ordered = append(ordered, items[i])
			i++
			continue
		}
		// A reasoning item between two calls belongs to the same parallel
		// batch; reasoning after the last call stays with what follows it.
		end := i
		for j := i; j < len(items) && (isResponsesToolCall(items[j]) || isResponsesReasoning(items[j])); j++ {
			if isResponsesToolCall(items[j]) {
				end = j + 1
			}
		}
		pending := make(map[string]bool)
		for ; i < end; i++ {
			ordered = append(ordered, items[i])
			if callID := responsesCallID(items[i]); isResponsesToolCall(items[i]) && callID != "" {
				pending[callID] = true
			}
		}
		for j := i; j < len(items) && len(pending) > 0; j++ {
			callID := strings.TrimSpace(kitutil.Interface2String(items[j]["call_id"]))
			if moved[j] || !isResponsesToolOutput(items[j]) || !pending[callID] {
				continue
			}
			ordered = append(ordered, items[j])
			moved[j] = true
			delete(pending, callID)
		}
	}
	return ordered
}

func isResponsesToolCall(item map[string]any) bool {
	itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
	return itemType == responsesInputTypeFunctionCall || itemType == responsesInputTypeCustomToolCall
}

func isResponsesReasoning(item map[string]any) bool {
	return strings.TrimSpace(kitutil.Interface2String(item["type"])) == responsesInputTypeReasoning
}

func isResponsesToolOutput(item map[string]any) bool {
	itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
	return itemType == responsesInputTypeFunctionCallOutput || itemType == responsesInputTypeCustomToolOutput
}

// omitTrailingAssistantText drops the assistant turns without tool calls that
// end the converted conversation after a user turn or tool result. In
// Responses they are history and the model answers with a new item; Claude and
// Gemini would continue them as a prefill, which newer models reject with a
// 400. Codex leaves them at the end of input when it retries a stream that
// failed after an assistant message completed.
func omitTrailingAssistantText[T any](c context.Context, turns []T, isUser, isAssistantText func(T) bool) []T {
	end := len(turns)
	for end > 0 && isAssistantText(turns[end-1]) {
		end--
	}
	if end == len(turns) || end == 0 || !isUser(turns[end-1]) {
		return turns
	}
	convdiag.Add(c, types.ConversionDiagnostic{
		Code:     "trailing_assistant_omitted",
		Path:     "input",
		Message:  "assistant output after the last user message or tool result was omitted; the target protocol would continue it as a prefill",
		Severity: types.ConversionDiagnosticWarning,
	})
	return turns[:end]
}

func responsesContentParts(content any) ([]map[string]any, error) {
	switch typed := content.(type) {
	case nil:
		return nil, nil
	case string:
		return []map[string]any{{"type": "input_text", "text": typed}}, nil
	case []map[string]any:
		return typed, nil
	case []any:
		parts := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			switch part := item.(type) {
			case string:
				parts = append(parts, map[string]any{"type": "input_text", "text": part})
			case map[string]any:
				parts = append(parts, part)
			default:
				raw, err := kitutil.Marshal(part)
				if err != nil {
					return nil, err
				}
				parts = append(parts, map[string]any{"type": "input_text", "text": string(raw)})
			}
		}
		return parts, nil
	default:
		raw, err := kitutil.Marshal(typed)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"type": "input_text", "text": string(raw)}}, nil
	}
}

func ContentParts(content any) ([]map[string]any, error) {
	return responsesContentParts(content)
}

func responsesRequestFunctionDeclarations(raw []byte) ([]dto.FunctionRequest, error) {
	if !rawJSONPresent(raw) {
		return nil, nil
	}

	var tools []map[string]any
	if err := kitutil.Unmarshal(raw, &tools); err != nil {
		return nil, fmt.Errorf("invalid tools: %w", err)
	}

	functions := make([]dto.FunctionRequest, 0, len(tools))
	for _, tool := range tools {
		if strings.TrimSpace(kitutil.Interface2String(tool["type"])) != "function" {
			continue
		}
		name := strings.TrimSpace(kitutil.Interface2String(tool["name"]))
		if name == "" {
			continue
		}
		var strict *bool
		if value, ok := tool["strict"].(bool); ok {
			strict = &value
		}
		functions = append(functions, dto.FunctionRequest{
			Strict:      strict,
			Name:        name,
			Description: kitutil.Interface2String(tool["description"]),
			Parameters:  tool["parameters"],
		})
	}
	return functions, nil
}

func RequestFunctionDeclarations(raw []byte) ([]dto.FunctionRequest, error) {
	return responsesRequestFunctionDeclarations(raw)
}

func responsesReasoningEffort(req *dto.OpenAIResponsesRequest) string {
	if req == nil || req.Reasoning == nil {
		return ""
	}
	return req.Reasoning.Effort
}

func ReasoningEffort(req *dto.OpenAIResponsesRequest) string {
	return responsesReasoningEffort(req)
}

func responsesObjectValue(value any, fallbackKey string) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	case string:
		var object map[string]any
		if err := kitutil.Unmarshal([]byte(typed), &object); err == nil {
			return object
		}
		var array []any
		if err := kitutil.Unmarshal([]byte(typed), &array); err == nil {
			return map[string]any{fallbackKey: array}
		}
		return map[string]any{fallbackKey: typed}
	case []any:
		return map[string]any{fallbackKey: typed}
	default:
		return map[string]any{fallbackKey: typed}
	}
}

func ObjectValue(value any, fallbackKey string) map[string]any {
	return responsesObjectValue(value, fallbackKey)
}

func responsesGeminiResponseMap(value any) map[string]any {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}
	case map[string]any:
		return typed
	case string:
		var object map[string]any
		if err := kitutil.Unmarshal([]byte(typed), &object); err == nil {
			return object
		}
		var array []any
		if err := kitutil.Unmarshal([]byte(typed), &array); err == nil {
			return map[string]any{"result": array}
		}
		return map[string]any{"content": typed}
	case []any:
		return map[string]any{"result": typed}
	default:
		return map[string]any{"content": typed}
	}
}

func GeminiResponseMap(value any) map[string]any {
	return responsesGeminiResponseMap(value)
}

func responsesParallelToolCalls(raw []byte) *bool {
	if !rawJSONPresent(raw) || kitutil.GetJsonType(raw) != "boolean" {
		return nil
	}
	var parallelToolCalls bool
	if err := kitutil.Unmarshal(raw, &parallelToolCalls); err != nil {
		return nil
	}
	return &parallelToolCalls
}

func ParallelToolCalls(raw []byte) *bool {
	return responsesParallelToolCalls(raw)
}

func ContentPartToFileSource(part map[string]any) types.FileSource {
	partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
	var data string
	var mimeType string

	switch partType {
	case "input_image":
		data, mimeType = responsesPartDataAndMime(part, "image_url", "url")
	case "input_file":
		data, mimeType = responsesPartDataAndMime(part, "file", "file_data", "file_url", "url")
	case "input_audio":
		data, mimeType = responsesPartDataAndMime(part, "input_audio", "data", "url")
		if mimeType == "" {
			if payload, ok := part["input_audio"].(map[string]any); ok {
				if format := strings.TrimSpace(kitutil.Interface2String(payload["format"])); format != "" {
					mimeType = "audio/" + format
				}
			}
		}
	case "input_video":
		data, mimeType = responsesPartDataAndMime(part, "video_url", "url")
	}
	if data == "" {
		return nil
	}
	return types.NewFileSourceFromData(data, mimeType)
}

func responsesPartDataAndMime(part map[string]any, keys ...string) (string, string) {
	mimeType := strings.TrimSpace(kitutil.Interface2String(part["mime_type"]))
	for _, key := range keys {
		value, ok := part[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case string:
			if typed != "" {
				return typed, mimeType
			}
		case map[string]any:
			if mimeType == "" {
				mimeType = strings.TrimSpace(kitutil.Interface2String(typed["mime_type"]))
			}
			for _, nestedKey := range []string{"url", "file_data", "file_url", "data"} {
				if data := strings.TrimSpace(kitutil.Interface2String(typed[nestedKey])); data != "" {
					return data, mimeType
				}
			}
		}
	}
	return "", mimeType
}
