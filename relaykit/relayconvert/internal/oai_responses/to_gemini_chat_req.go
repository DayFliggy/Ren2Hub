package oairesponses

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"context"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/internal/convdiag"
	relaymedia "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/media"
	sharedgemini "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/shared/gemini"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
)

func OpenAIResponsesRequestToGeminiChat(c context.Context, req *dto.OpenAIResponsesRequest, info convmeta.Meta) (*dto.GeminiChatRequest, error) {
	opts := convmeta.OptionsOf(info)
	if req == nil {
		return nil, fmt.Errorf("request is nil")
	}
	if req.Model == "" {
		return nil, fmt.Errorf("model is required")
	}
	if err := ValidateRequestChatUnsupportedFields(req); err != nil {
		return nil, err
	}

	geminiRequest := &dto.GeminiChatRequest{
		GenerationConfig: dto.GeminiChatGenerationConfig{
			Temperature: req.Temperature,
		},
	}
	if req.TopP != nil {
		geminiRequest.GenerationConfig.TopP = kitutil.GetPointer(*req.TopP)
	}
	if req.MaxOutputTokens != nil {
		geminiRequest.GenerationConfig.MaxOutputTokens = kitutil.GetPointer(*req.MaxOutputTokens)
	}

	upstreamModelName := req.Model
	if modelName := convmeta.UpstreamModelName(info); modelName != "" {
		upstreamModelName = modelName
	}
	if opts.Gemini.SupportsImagineModel(upstreamModelName) {
		geminiRequest.GenerationConfig.ResponseModalities = []string{"TEXT", "IMAGE"}
	}
	if err := applyResponsesTextToGemini(req.Text, geminiRequest); err != nil {
		return nil, err
	}
	reasoningIntent, diagnostics, err := reasoning.FromOpenAIResponses(req)
	if err != nil {
		return nil, reasoning.AsClientError(err)
	}
	convdiag.Add(c, diagnostics...)
	var reasoningPivot dto.GeneralOpenAIRequest
	if err := reasoning.ApplyToOpenAIChat(&reasoningPivot, reasoningIntent); err != nil {
		return nil, reasoning.AsClientError(err)
	}
	reasoningPivot.Model = req.Model
	reasoningPivot.MaxCompletionTokens = req.MaxOutputTokens
	if err := sharedgemini.ApplyThinkingConfig(c, geminiRequest, info, reasoningPivot); err != nil {
		return nil, reasoning.AsClientError(err)
	}

	var safetySettings []dto.GeminiChatSafetySettings
	for _, category := range sharedgemini.SafetySettingCategories {
		threshold := opts.Gemini.SafetySettingFor(category)
		if threshold == "" {
			continue
		}
		safetySettings = append(safetySettings, dto.GeminiChatSafetySettings{
			Category:  category,
			Threshold: threshold,
		})
	}
	if len(safetySettings) > 0 {
		geminiRequest.SafetySettings = safetySettings
	}

	functions, err := RequestFunctionDeclarations(req.Tools)
	if err != nil {
		return nil, err
	}
	for i := range functions {
		if params, ok := functions[i].Parameters.(map[string]any); ok {
			if props, hasProps := params["properties"].(map[string]any); hasProps && len(props) == 0 {
				functions[i].Parameters = nil
				continue
			}
		}
		functions[i].Parameters = sharedgemini.CleanFunctionParameters(functions[i].Parameters)
	}
	if len(functions) > 0 {
		geminiRequest.SetTools([]dto.GeminiChatTool{
			{FunctionDeclarations: functions},
		})
	}

	toolChoice, err := RequestToolChoiceToChat(req.ToolChoice)
	if err != nil {
		return nil, err
	}
	if toolChoice != nil {
		geminiRequest.ToolConfig = sharedgemini.OpenAIToolChoiceToConfig(toolChoice)
	}

	systemTexts := make([]string, 0)
	if RawJSONPresent(req.Instructions) {
		instructions, err := JSONString(req.Instructions)
		if err != nil {
			return nil, fmt.Errorf("invalid instructions: %w", err)
		}
		if strings.TrimSpace(instructions) != "" {
			systemTexts = append(systemTexts, instructions)
		}
	}

	inputItems, err := InputItems(req.Input)
	if err != nil {
		return nil, err
	}
	callNames := make(map[string]string)
	// Multimodal function responses (media in functionResponse.parts) are
	// documented for Gemini 3. Gemini 1.x and 2.x get tool media as ordinary
	// inlineData parts after the function responses of the same turn.
	modelID := path.Base(strings.ToLower(upstreamModelName))
	toolMediaInFunctionResponse := !strings.HasPrefix(modelID, "gemini-1.") && !strings.HasPrefix(modelID, "gemini-2.")
	for _, item := range inputItems {
		itemType := strings.TrimSpace(kitutil.Interface2String(item["type"]))
		switch itemType {
		case ResponsesInputTypeFunctionCall, ResponsesInputTypeCustomToolCall:
			part, callID, err := responsesFunctionCallItemToGeminiPart(item, itemType)
			if err != nil {
				return nil, err
			}
			sharedgemini.AttachFunctionCallThoughtSignature(opts, &part)
			if callID != "" {
				callNames[callID] = part.FunctionCall.FunctionName
			}
			appendGeminiContentPart(geminiRequest, "model", part)
		case ResponsesInputTypeFunctionCallOutput, ResponsesInputTypeCustomToolOutput:
			part, media, err := responsesFunctionOutputItemToGeminiPart(c, item, callNames, toolMediaInFunctionResponse)
			if err != nil {
				return nil, err
			}
			appendGeminiContentPart(geminiRequest, "user", part)
			turn := &geminiRequest.Contents[len(geminiRequest.Contents)-1]
			turn.Parts = append(turn.Parts, media...)
		default:
			role := responsesGeminiRole(item)
			parts, err := responsesInputContentToGeminiParts(c, item["content"])
			if err != nil {
				return nil, err
			}
			if role == "system" {
				for _, part := range parts {
					if part.Text != "" {
						systemTexts = append(systemTexts, part.Text)
					}
				}
				continue
			}
			if len(parts) > 0 {
				geminiRequest.Contents = append(geminiRequest.Contents, dto.GeminiChatContent{
					Role:  role,
					Parts: parts,
				})
			}
		}
	}

	if len(systemTexts) > 0 {
		geminiRequest.SystemInstructions = &dto.GeminiChatContent{
			Parts: []dto.GeminiPart{{Text: strings.Join(systemTexts, "\n")}},
		}
	}

	return geminiRequest, nil
}

func applyResponsesTextToGemini(raw []byte, geminiRequest *dto.GeminiChatRequest) error {
	responseFormat, err := RequestTextToChatResponseFormat(raw)
	if err != nil {
		return err
	}
	if responseFormat == nil || (responseFormat.Type != "json_schema" && responseFormat.Type != "json_object") {
		return nil
	}

	geminiRequest.GenerationConfig.ResponseMimeType = "application/json"
	if len(responseFormat.JsonSchema) == 0 {
		return nil
	}

	var jsonSchema dto.FormatJsonSchema
	if err := kitutil.Unmarshal(responseFormat.JsonSchema, &jsonSchema); err != nil {
		return nil
	}
	geminiRequest.GenerationConfig.ResponseSchema = sharedgemini.RemoveAdditionalProperties(jsonSchema.Schema, 0)
	return nil
}

func responsesInputContentToGeminiParts(c context.Context, content any) ([]dto.GeminiPart, error) {
	contentParts, err := ContentParts(content)
	if err != nil {
		return nil, err
	}

	parts := make([]dto.GeminiPart, 0, len(contentParts))
	for _, contentPart := range contentParts {
		nextParts, err := responsesContentPartToGeminiParts(c, contentPart)
		if err != nil {
			return nil, err
		}
		parts = append(parts, nextParts...)
	}
	return parts, nil
}

func responsesContentPartToGeminiParts(c context.Context, part map[string]any) ([]dto.GeminiPart, error) {
	partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
	switch partType {
	case "input_text", "output_text", "text":
		text := kitutil.Interface2String(part["text"])
		if text == "" {
			return nil, nil
		}
		return []dto.GeminiPart{{Text: text}}, nil
	case "input_image", "input_file", "input_audio", "input_video":
		source := ContentPartToFileSource(part)
		if source == nil {
			return nil, nil
		}
		base64Data, mimeType, err := relaymedia.ResolveBase64Data(c, source, "formatting Responses input for Gemini")
		if err != nil {
			return nil, fmt.Errorf("get file data from '%s' failed: %w", source.GetIdentifier(), err)
		}
		if !sharedgemini.IsSupportedMimeType(mimeType) {
			return nil, fmt.Errorf("mime type is not supported by Gemini: '%s', url: '%s', supported types are: %v", mimeType, source.GetIdentifier(), sharedgemini.SupportedMimeTypesList())
		}
		return []dto.GeminiPart{
			{
				InlineData: &dto.GeminiInlineData{
					MimeType: mimeType,
					Data:     base64Data,
				},
			},
		}, nil
	default:
		return nil, nil
	}
}

func responsesFunctionCallItemToGeminiPart(item map[string]any, itemType string) (dto.GeminiPart, string, error) {
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		return dto.GeminiPart{}, "", fmt.Errorf("%s item is missing name", itemType)
	}
	var arguments map[string]any
	if itemType == ResponsesInputTypeCustomToolCall {
		// The custom tool is declared as a function taking one string
		// argument, so its raw input is replayed in that shape.
		arguments = map[string]any{convmeta.CustomToolInputArgument: responsesArgumentsString(item["input"])}
	} else {
		arguments = ObjectValue(item["arguments"], "arguments")
	}
	callID := CallID(item)
	return dto.GeminiPart{
		FunctionCall: &dto.FunctionCall{
			ID:           callID,
			FunctionName: name,
			Arguments:    arguments,
		},
	}, callID, nil
}

// responsesFunctionOutputItemToGeminiPart returns the functionResponse part
// and, unless mediaInParts puts them into functionResponse.parts, the tool
// media the caller sends as separate parts after the turn's function
// responses.
func responsesFunctionOutputItemToGeminiPart(c context.Context, item map[string]any, callNames map[string]string, mediaInParts bool) (dto.GeminiPart, []dto.GeminiPart, error) {
	callID := CallID(item)
	name := strings.TrimSpace(kitutil.Interface2String(item["name"]))
	if name == "" {
		name = callNames[callID]
	}
	output, media, err := responsesToolOutputToGemini(c, item["output"])
	if err != nil {
		return dto.GeminiPart{}, nil, err
	}
	response := &dto.GeminiFunctionResponse{
		Name:     name,
		Response: output,
	}
	if mediaInParts && len(media) > 0 {
		parts, err := kitutil.Marshal(media)
		if err != nil {
			return dto.GeminiPart{}, nil, fmt.Errorf("failed to marshal function response parts: %w", err)
		}
		response.Parts = parts
		media = nil
	}
	if callID != "" {
		id, err := kitutil.Marshal(callID)
		if err != nil {
			return dto.GeminiPart{}, nil, fmt.Errorf("failed to marshal function response ID: %w", err)
		}
		response.ID = id
	}
	return dto.GeminiPart{
		FunctionResponse: response,
	}, media, nil
}

// responsesToolOutputToGemini maps a function_call_output payload onto a Gemini
// functionResponse. A Responses content-part array keeps its text in response
// and returns its images and documents, in order, as inlineData parts;
// stringifying them would hand base64 media to the upstream text tokenizer.
// Media Gemini cannot carry for a tool result fails the conversion instead of
// being dropped. A payload without media keeps the historical response map;
// media anywhere else (a single part object, or beside an unknown part) also
// fails, so this converter never relies on planning to stop stringified media.
func responsesToolOutputToGemini(c context.Context, value any) (map[string]any, []dto.GeminiPart, error) {
	rawParts, ok := value.([]any)
	if !ok || len(rawParts) == 0 {
		if responsesToolOutputHasMedia(value) {
			return nil, nil, fmt.Errorf("tool output media must be a list of content parts")
		}
		return GeminiResponseMap(value), nil, nil
	}
	for _, rawPart := range rawParts {
		part, isMap := rawPart.(map[string]any)
		if !isMap || !slices.Contains([]string{"input_text", "output_text", "text", "input_image", "input_file", "input_audio", "input_video"}, strings.TrimSpace(kitutil.Interface2String(part["type"]))) {
			if responsesToolOutputHasMedia(value) {
				return nil, nil, fmt.Errorf("tool output mixes media with parts gemini cannot represent")
			}
			return GeminiResponseMap(value), nil, nil
		}
	}

	texts := make([]string, 0, len(rawParts))
	media := make([]dto.GeminiPart, 0, len(rawParts))
	for _, rawPart := range rawParts {
		part := rawPart.(map[string]any)
		partType := strings.TrimSpace(kitutil.Interface2String(part["type"]))
		switch partType {
		case "input_text", "output_text", "text":
			if text := kitutil.Interface2String(part["text"]); text != "" {
				texts = append(texts, text)
			}
			continue
		}
		parts, err := responsesContentPartToGeminiParts(c, part)
		if err != nil {
			return nil, nil, err
		}
		if len(parts) == 0 {
			return nil, nil, fmt.Errorf("tool output %s part has no media source", partType)
		}
		// Gemini documents only images, PDF and plain text in function
		// responses; audio, video and other inline types fail before send.
		mimeType := strings.ToLower(parts[0].InlineData.MimeType)
		if !sharedgemini.IsSupportedToolResultMimeType(mimeType) {
			return nil, nil, fmt.Errorf("gemini tool results cannot carry %s content", mimeType)
		}
		media = append(media, parts...)
	}
	if len(texts) == 0 {
		return map[string]any{}, media, nil
	}
	return GeminiResponseMap(strings.Join(texts, "\n")), media, nil
}

func appendGeminiContentPart(req *dto.GeminiChatRequest, role string, part dto.GeminiPart) {
	if len(req.Contents) > 0 && req.Contents[len(req.Contents)-1].Role == role {
		parts := req.Contents[len(req.Contents)-1].Parts
		insertAt := len(parts)
		switch {
		case role == "model" && part.FunctionCall != nil:
			insertAt = 0
			for insertAt < len(parts) && parts[insertAt].FunctionCall != nil {
				insertAt++
			}
		case part.FunctionResponse != nil:
			// Tool media sent as separate parts stay after every function
			// response of the turn, so a later response goes before them.
			for i := len(parts) - 1; i >= 0; i-- {
				if parts[i].FunctionResponse != nil {
					insertAt = i + 1
					break
				}
			}
		}
		req.Contents[len(req.Contents)-1].Parts = slices.Insert(parts, insertAt, part)
		return
	}
	req.Contents = append(req.Contents, dto.GeminiChatContent{
		Role:  role,
		Parts: []dto.GeminiPart{part},
	})
}

func responsesGeminiRole(item map[string]any) string {
	switch strings.TrimSpace(kitutil.Interface2String(item["role"])) {
	case "assistant":
		return "model"
	case "system", "developer":
		return "system"
	case "model":
		return "model"
	default:
		return "user"
	}
}

// responsesToolOutputHasMedia reports whether a tool output payload carries a
// media part anywhere, so a shape the converter cannot map never falls back to
// a response map that would hold the media as text.
func responsesToolOutputHasMedia(value any) bool {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if responsesToolOutputHasMedia(item) {
				return true
			}
		}
	case map[string]any:
		switch strings.TrimSpace(kitutil.Interface2String(typed["type"])) {
		case "input_image", "input_file", "input_audio", "input_video":
			return true
		}
		for _, nested := range typed {
			if responsesToolOutputHasMedia(nested) {
				return true
			}
		}
	}
	return false
}
