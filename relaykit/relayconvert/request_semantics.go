package relayconvert

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/toolconv"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// These envelopes describe protocol fields whose DTO representation is open.
// Schemas, arguments, results and user metadata deliberately have no envelope.
type responsesInputEnvelope struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	Role             string          `json:"role"`
	Content          any             `json:"content"`
	Name             string          `json:"name"`
	Namespace        *string         `json:"namespace"`
	CallID           string          `json:"call_id"`
	Arguments        json.RawMessage `json:"arguments"`
	Input            json.RawMessage `json:"input"`
	Output           json.RawMessage `json:"output"`
	Status           string          `json:"status"`
	Summary          json.RawMessage `json:"summary"`
	EncryptedContent json.RawMessage `json:"encrypted_content"`
}

type responsesContentEnvelope struct {
	Type        string          `json:"type"`
	Text        string          `json:"text"`
	ImageURL    string          `json:"image_url"`
	Detail      string          `json:"detail"`
	FileID      string          `json:"file_id"`
	FileURL     string          `json:"file_url"`
	FileData    string          `json:"file_data"`
	Filename    string          `json:"filename"`
	Refusal     string          `json:"refusal"`
	Annotations json.RawMessage `json:"annotations"`
	InputAudio  json.RawMessage `json:"input_audio"`
	VideoURL    json.RawMessage `json:"video_url"`
}

type responsesTextEnvelope struct {
	Format    any    `json:"format"`
	Verbosity string `json:"verbosity"`
}

type responsesFormatEnvelope struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Schema      json.RawMessage `json:"schema"`
	Strict      *bool           `json:"strict"`
}

type functionToolEnvelope struct {
	dto.FunctionRequest
	Type        string          `json:"type"`
	InputSchema json.RawMessage `json:"input_schema"`
	Format      json.RawMessage `json:"format"`
}

type toolChoiceEnvelope struct {
	Type                   string `json:"type"`
	Name                   string `json:"name"`
	Function               any    `json:"function"`
	DisableParallelToolUse *bool  `json:"disable_parallel_tool_use"`
}

type namedFunctionEnvelope struct {
	Name string `json:"name"`
}

func rawProtocolEnvelopeType(format types.RelayFormat, path string) reflect.Type {
	var normalized strings.Builder
	for index := 0; index < len(path); index++ {
		if path[index] == '[' {
			normalized.WriteString("[]")
			for index+1 < len(path) && path[index+1] != ']' {
				index++
			}
			index++
			continue
		}
		normalized.WriteByte(path[index])
	}
	path = normalized.String()
	if path == "tool_choice" {
		return reflect.TypeFor[toolChoiceEnvelope]()
	}
	if path == "tool_choice.function" {
		return reflect.TypeFor[namedFunctionEnvelope]()
	}
	switch format {
	case types.RelayFormatOpenAI:
		switch path {
		case "messages[].content":
			return reflect.TypeFor[[]dto.MediaContent]()
		case "messages[].tool_calls":
			return reflect.TypeFor[[]dto.ToolCallRequest]()
		case "messages[].content[].image_url":
			return reflect.TypeFor[dto.MessageImageUrl]()
		case "messages[].content[].input_audio":
			return reflect.TypeFor[dto.MessageInputAudio]()
		case "messages[].content[].file":
			return reflect.TypeFor[dto.MessageFile]()
		case "messages[].content[].video_url":
			return reflect.TypeFor[dto.MessageVideoUrl]()
		case "response_format.json_schema":
			return reflect.TypeFor[dto.FormatJsonSchema]()
		case "functions":
			return reflect.TypeFor[[]dto.FunctionRequest]()
		}
	case types.RelayFormatClaude:
		if path == "messages[].content" || path == "system" || strings.HasSuffix(path, ".content[].content") {
			return reflect.TypeFor[[]dto.ClaudeMediaMessage]()
		}
		if path == "tools" {
			return reflect.TypeFor[[]functionToolEnvelope]()
		}
	case types.RelayFormatOpenAIResponses:
		switch path {
		case "input":
			return reflect.TypeFor[[]responsesInputEnvelope]()
		case "input[].content":
			return reflect.TypeFor[[]responsesContentEnvelope]()
		case "text":
			return reflect.TypeFor[responsesTextEnvelope]()
		case "text.format":
			return reflect.TypeFor[responsesFormatEnvelope]()
		case "tools":
			return reflect.TypeFor[[]functionToolEnvelope]()
		}
	case types.RelayFormatGemini:
		switch path {
		case "tools":
			return reflect.TypeFor[[]dto.GeminiChatTool]()
		case "tools[].functionDeclarations":
			return reflect.TypeFor[[]dto.FunctionRequest]()
		}
	}
	return nil
}

func (r *RequestRequirements) inspectRetainedRequestFields(request any, payload map[string]any) error {
	collector := requestFieldCollector{fields: make(map[reflect.Type]map[string]reflect.Type), format: r.Format, inspectRetained: true}
	if err := collector.inspect(reflect.TypeOf(request), payload, "", 0); err != nil {
		return err
	}
	slices.Sort(collector.unknown)
	for _, path := range collector.unknown {
		r.Diagnostics = append(r.Diagnostics, types.ConversionDiagnostic{
			Code: "unknown_conversion_field", Path: path, Severity: types.ConversionDiagnosticWarning,
			Message: "unrecognized protocol envelope field is not guaranteed to survive conversion", From: r.Format,
		})
	}
	return nil
}

func (r *RequestRequirements) inspectProjectionSemantics(payload map[string]any) error {
	if r.Format == types.RelayFormatOpenAI {
		messages, _ := payload["messages"].([]any)
		for index, value := range messages {
			message, _ := value.(map[string]any)
			for _, field := range []string{"refusal", "audio"} {
				if message[field] != nil {
					r.projectionLoss("chat_output_history_not_portable", fmt.Sprintf("messages[%d].%s", index, field), "conversion cannot preserve Chat refusal or audio history", "")
				}
			}
		}
	}

	if r.Format == types.RelayFormatOpenAIResponses {
		items, _ := payload["input"].([]any)
		for index, item := range items {
			call, _ := item.(map[string]any)
			if call["type"] != "function_call" || call["namespace"] == nil {
				continue
			}
			path := fmt.Sprintf("input[%d].namespace", index)
			namespace, ok := call["namespace"].(string)
			if !ok || strings.TrimSpace(namespace) == "" {
				return fmt.Errorf("%s must be a non-empty string or null", path)
			}
			// Historical tool identity has the same portability boundary as a
			// namespace declaration, even when no tools are declared this turn.
			r.Tools = append(r.Tools, ToolRequirement{Type: "namespace", Path: path})
			r.Features = appendUnique(r.Features, "tools")
		}
	}
	if choice, ok := payload["tool_choice"].(map[string]any); ok {
		kind, _ := choice["type"].(string)
		allowed := []string{"function"}
		if r.Format == types.RelayFormatOpenAIResponses {
			allowed = append(allowed, "custom")
		}
		if r.Format == types.RelayFormatClaude {
			allowed = []string{"auto", "none", "any", "tool"}
		}
		if !slices.Contains(allowed, kind) {
			r.projectionLoss("unsupported_tool_choice", "tool_choice.type", "conversion cannot enforce this tool selection mode", "")
		}
	} else if choice, ok := payload["tool_choice"].(string); ok && !slices.Contains([]string{"auto", "none", "required"}, choice) {
		r.projectionLoss("unsupported_tool_choice", "tool_choice", "conversion cannot enforce this tool selection mode", "")
	}
	switch r.Format {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses:
		format, _ := payload["response_format"].(map[string]any)
		path := "response_format"
		if r.Format == types.RelayFormatOpenAIResponses {
			text, _ := payload["text"].(map[string]any)
			format, _ = text["format"].(map[string]any)
			path = "text.format"
		}
		kind, _ := format["type"].(string)
		if kind != "" && kind != "text" {
			r.Features = appendUnique(r.Features, "structured_output")
			r.projectionLoss("structured_output_not_portable", path, "Claude conversion does not preserve the requested output format", types.RelayFormatClaude)
			if kind == "json_schema" {
				definition := format
				if r.Format == types.RelayFormatOpenAI {
					definition, _ = format["json_schema"].(map[string]any)
					path += ".json_schema"
				}
				if strict, ok := definition["strict"].(bool); ok {
					severity := types.ConversionDiagnosticWarning
					if strict {
						severity = types.ConversionDiagnosticError
					}
					r.Diagnostics = append(r.Diagnostics, types.ConversionDiagnostic{Code: "strict_output_not_portable", Path: path + ".strict", Message: "Gemini conversion cannot preserve explicit JSON Schema strictness", Severity: severity, From: r.Format, To: types.RelayFormatGemini})
				}
				encoded, err := kitutil.Marshal(definition["schema"])
				if err != nil {
					return err
				}
				var cloned any
				if err := kitutil.Unmarshal(encoded, &cloned); err != nil {
					return err
				}
				if err := r.inspectSchemaProjection(definition["schema"], sharedgemini.RemoveAdditionalProperties(cloned, 0), path+".schema", 0); err != nil {
					return err
				}
			}
		}
	case types.RelayFormatClaude:
		messages, _ := payload["messages"].([]any)
		if _, present := payload["safeguards"]; present {
			r.projectionLoss("claude_safeguards_not_portable", "safeguards", "conversion cannot preserve Claude safeguards", "")
		}
		for index, item := range messages {
			message, _ := item.(map[string]any)
			if _, exists := message["output_config"]; exists {
				r.projectionLoss("claude_message_output_config_not_portable", fmt.Sprintf("messages[%d].output_config", index), "conversion cannot preserve Claude per-message output configuration", "")
			}
		}
		if valuePresent(payload["output_format"]) {
			r.Features = appendUnique(r.Features, "structured_output")
			r.projectionLoss("structured_output_not_portable", "output_format", "conversion does not preserve Claude output constraints", "")
		}
		config, _ := payload["output_config"].(map[string]any)
		if valuePresent(config["format"]) {
			r.Features = appendUnique(r.Features, "structured_output")
			r.projectionLoss("structured_output_not_portable", "output_config.format", "conversion does not preserve Claude output constraints", "")
		}
		choice, _ := payload["tool_choice"].(map[string]any)
		if disabled, _ := choice["disable_parallel_tool_use"].(bool); disabled {
			r.Features = appendUnique(r.Features, "serial_tools")
		}
	case types.RelayFormatGemini:
		toolConfig, _ := payload["toolConfig"].(map[string]any)
		functionConfig, _ := toolConfig["functionCallingConfig"].(map[string]any)
		mode, _ := functionConfig["mode"].(string)
		if !slices.Contains([]string{"", "AUTO", "NONE", "ANY"}, mode) {
			r.projectionLoss("unsupported_tool_choice", "toolConfig.functionCallingConfig.mode", "conversion cannot enforce this tool selection mode", "")
		}
		if names, _ := functionConfig["allowedFunctionNames"].([]any); len(names) != 0 && mode != "ANY" {
			r.projectionLoss("unsupported_tool_choice", "toolConfig.functionCallingConfig.allowedFunctionNames", "conversion only preserves allowed functions in ANY mode", "")
		}
		config, _ := payload["generationConfig"].(map[string]any)
		for _, field := range []string{"responseSchema", "responseJsonSchema", "responseMimeType"} {
			if valuePresent(config[field]) && !(field == "responseMimeType" && config[field] == "text/plain") {
				r.Features = appendUnique(r.Features, "structured_output")
				r.projectionLoss("structured_output_not_portable", "generationConfig."+field, "conversion does not preserve Gemini output constraints", "")
			}
		}
	}
	return nil
}

func (r *RequestRequirements) inspectToolSchema(schema any, path string) error {
	field := ".parameters"
	if r.Format == types.RelayFormatClaude {
		field = ".input_schema"
	} else if r.Format == types.RelayFormatOpenAI {
		field = ".function.parameters"
	}
	return r.inspectSchemaProjection(schema, sharedgemini.CleanFunctionParameters(schema), path+field, 0)
}

func (r *RequestRequirements) projectionLoss(code, path, message string, target types.RelayFormat) {
	r.Diagnostics = append(r.Diagnostics, types.ConversionDiagnostic{Code: code, Path: path, Message: message, Severity: types.ConversionDiagnosticError, From: r.Format, To: target})
}

// inspectLegacyFunctionHistory declares legacy Chat function calls and results
// that conversion cannot pair. Paired history converts as a tool exchange.
func (r *RequestRequirements) inspectLegacyFunctionHistory(request any) error {
	var messages []dto.Message
	switch request := request.(type) {
	case *dto.GeneralOpenAIRequest:
		if request != nil {
			messages = request.Messages
		}
	case dto.GeneralOpenAIRequest:
		messages = request.Messages
	}
	diagnostics, err := toolconv.LegacyFunctionHistoryDiagnostics(messages)
	if err != nil {
		return err
	}
	for _, diagnostic := range diagnostics {
		diagnostic.From = r.Format
		r.Diagnostics = append(r.Diagnostics, diagnostic)
	}
	return nil
}

func (r *RequestRequirements) inspectSchemaProjection(original, converted any, path string, depth int) error {
	if len(r.Diagnostics) >= 128 || len(path) > 1024 {
		return fmt.Errorf("request exceeds conversion diagnostic limits")
	}
	if depth > 64 {
		return fmt.Errorf("schema exceeds the conversion depth limit")
	}
	switch original := original.(type) {
	case map[string]any:
		projected, _ := converted.(map[string]any)
		keys := make([]string, 0, len(original))
		for key := range original {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		for _, key := range keys {
			if len(r.Diagnostics) >= 128 {
				return fmt.Errorf("request exceeds conversion diagnostic limits")
			}
			value := original[key]
			childPath := path + "." + key
			if len(childPath) > 1024 {
				return fmt.Errorf("request exceeds conversion diagnostic limits")
			}
			if _, exists := projected[key]; !exists {
				severity := types.ConversionDiagnosticError
				if key == "title" || key == "description" || key == "examples" || key == "$comment" {
					severity = types.ConversionDiagnosticWarning
				}
				r.Diagnostics = append(r.Diagnostics, types.ConversionDiagnostic{Code: "schema_field_not_portable", Path: childPath, Message: "Gemini conversion cannot preserve these JSON Schema constraints or annotations", Severity: severity, From: r.Format, To: types.RelayFormatGemini})
				continue
			}
			if key == "properties" {
				properties, _ := value.(map[string]any)
				convertedProperties, _ := projected[key].(map[string]any)
				names := make([]string, 0, len(properties))
				for name := range properties {
					names = append(names, name)
				}
				slices.Sort(names)
				for _, name := range names {
					if err := r.inspectSchemaProjection(properties[name], convertedProperties[name], childPath+"."+name, depth+1); err != nil {
						return err
					}
				}
			} else if key == "items" || key == "anyOf" || key == "allOf" || key == "oneOf" {
				if err := r.inspectSchemaProjection(value, projected[key], childPath, depth+1); err != nil {
					return err
				}
			} else if key == "type" {
				if _, unchanged := projected[key].([]any); unchanged {
					continue
				}
				if choices, ok := value.([]any); ok {
					nonNull := make(map[string]bool)
					for _, item := range choices {
						if kind, ok := item.(string); ok && !strings.EqualFold(kind, "null") {
							nonNull[strings.ToLower(kind)] = true
						}
					}
					if len(nonNull) > 1 {
						r.projectionLoss("schema_union_not_portable", childPath, "Gemini function schema conversion retains only one non-null type", types.RelayFormatGemini)
					}
				}
			}
		}
	case []any:
		projected, ok := converted.([]any)
		if !ok || len(projected) != len(original) {
			r.projectionLoss("schema_tuple_not_portable", path, "Gemini schema conversion does not preserve all tuple entries", types.RelayFormatGemini)
			return nil
		}
		for index, value := range original {
			if err := r.inspectSchemaProjection(value, projected[index], fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}
