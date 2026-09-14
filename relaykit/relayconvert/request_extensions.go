package relayconvert

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// AnalyzeRequestJSON supplements DTO analysis with fields that the
// decoder would discard. Call it with the original JSON used to decode request.
// Only names are retained; diagnostic metadata never contains extension values.
func AnalyzeRequestJSON(request any, original []byte) (RequestRequirements, error) {
	requirements, err := AnalyzeRequest(request)
	if err != nil {
		return requirements, err
	}
	var payload map[string]any
	if err := kitutil.Unmarshal(original, &payload); err != nil {
		return requirements, err
	}
	if payload == nil {
		return requirements, fmt.Errorf("request must be a JSON object")
	}
	collector := requestFieldCollector{fields: make(map[reflect.Type]map[string]reflect.Type)}
	if err := collector.inspect(reflect.TypeOf(request), payload, "", 0); err != nil {
		return requirements, err
	}
	slices.Sort(collector.unknown)
	for _, field := range collector.unknown {
		severity := types.ConversionDiagnosticWarning
		if strings.HasPrefix(field, "tools[") && (strings.HasSuffix(field, ".defer_loading") || strings.HasSuffix(field, ".allowed_callers")) {
			severity = types.ConversionDiagnosticError
		}
		requirements.Diagnostics = append(requirements.Diagnostics, types.ConversionDiagnostic{
			Code: "unknown_request_field", Path: field, Message: "unrecognized request field is discarded by DTO forwarding or conversion",
			Severity: severity, From: requirements.Format,
		})
	}
	return requirements, nil
}

type requestFieldCollector struct {
	fields          map[reflect.Type]map[string]reflect.Type
	unknown         []string
	format          types.RelayFormat
	inspectRetained bool
}

func (c *requestFieldCollector) inspect(typ reflect.Type, value any, path string, depth int) error {
	if value == nil {
		return nil
	}
	if depth > 64 {
		return fmt.Errorf("request DTO nesting exceeds 64 levels")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ == reflect.TypeFor[json.RawMessage]() || typ.Kind() == reflect.Interface {
		if !c.inspectRetained {
			return nil
		}
		typ = rawProtocolEnvelopeType(c.format, path)
		if typ == nil {
			return nil
		}
	}
	switch typ.Kind() {
	case reflect.Struct:
		payload, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := c.fields[typ]
		if fields == nil {
			fields = make(map[string]reflect.Type)
			for _, field := range reflect.VisibleFields(typ) {
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if !field.IsExported() || name == "-" || (field.Anonymous && name == "") {
					continue
				}
				if name == "" {
					name = field.Name
				}
				fields[strings.ToLower(name)] = field.Type
			}
			for alias, canonical := range requestFieldAliases(typ) {
				fields[alias] = fields[strings.ToLower(canonical)]
			}
			c.fields[typ] = fields
		}
		for name, child := range payload {
			childPath := name
			if path != "" {
				childPath = path + "." + name
			}
			fieldType := fields[strings.ToLower(name)]
			if fieldType == nil {
				if len(c.unknown) >= 128 || len(childPath) > 1024 {
					return fmt.Errorf("request exceeds extension diagnostic limits")
				}
				c.unknown = append(c.unknown, childPath)
				continue
			}
			if err := c.inspect(fieldType, child, childPath, depth+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		if items, ok := value.([]any); ok {
			for index, child := range items {
				if err := c.inspect(typ.Elem(), child, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
					return err
				}
			}
		}
	case reflect.Map:
		if values, ok := value.(map[string]any); ok {
			for key, child := range values {
				if err := c.inspect(typ.Elem(), child, path+"."+key, depth+1); err != nil {
					return err
				}
			}
		}
	}
	// Arbitrary schemas, tool arguments and results remain open. Only registered
	// protocol envelopes inside RawMessage or interfaces have a closed shape.
	return nil
}

func requestFieldAliases(typ reflect.Type) map[string]string {
	switch typ {
	case reflect.TypeFor[dto.GeminiChatRequest]():
		return map[string]string{"system_instruction": "systemInstruction"}
	case reflect.TypeFor[dto.GeminiPart]():
		return map[string]string{"inline_data": "inlineData"}
	case reflect.TypeFor[dto.GeminiInlineData]():
		return map[string]string{"mime_type": "mimeType"}
	case reflect.TypeFor[dto.GeminiThinkingConfig]():
		return map[string]string{"include_thoughts": "includeThoughts", "thinking_budget": "thinkingBudget", "thinking_level": "thinkingLevel"}
	case reflect.TypeFor[dto.GeminiChatGenerationConfig]():
		return map[string]string{
			"top_p": "topP", "top_k": "topK", "max_output_tokens": "maxOutputTokens", "candidate_count": "candidateCount",
			"stop_sequences": "stopSequences", "response_mime_type": "responseMimeType", "response_schema": "responseSchema",
			"response_json_schema": "responseJsonSchema", "presence_penalty": "presencePenalty", "frequency_penalty": "frequencyPenalty",
			"response_logprobs": "responseLogprobs", "enable_enhanced_civic_answers": "enableEnhancedCivicAnswers",
			"media_resolution": "mediaResolution", "response_modalities": "responseModalities", "thinking_config": "thinkingConfig",
			"speech_config": "speechConfig", "image_config": "imageConfig",
		}
	}
	return nil
}
