package relayconvert

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type RequestRequirements struct {
	Format         types.RelayFormat            `json:"format"`
	Model          string                       `json:"model,omitempty"`
	Stream         bool                         `json:"stream"`
	Operation      string                       `json:"operation"`
	Features       []string                     `json:"features,omitempty"`
	NativeRequired bool                         `json:"native_required"`
	NativeState    []string                     `json:"native_state,omitempty"`
	Tools          []ToolRequirement            `json:"tools,omitempty"`
	Media          []MediaRequirement           `json:"media,omitempty"`
	Diagnostics    []types.ConversionDiagnostic `json:"diagnostics,omitempty"`
	Shape          *RequestShape                `json:"shape,omitempty"`
	ToolIdentity   *ToolIdentityRequirements    `json:"tool_identity,omitempty"`
}

type ToolRequirement struct {
	Type       string `json:"type"`
	Path       string `json:"path"`
	Configured bool   `json:"configured,omitempty"`
}

type ConversionPlan struct {
	From              types.RelayFormat            `json:"from"`
	To                types.RelayFormat            `json:"to"`
	Native            bool                         `json:"native"`
	RequestProjection string                       `json:"request_projection,omitempty"`
	RequestConverter  string                       `json:"request_converter,omitempty"`
	ResponseConverter string                       `json:"response_converter,omitempty"`
	StreamSupported   bool                         `json:"stream_supported"`
	Policy            types.ConversionLossPolicy   `json:"policy"`
	Steps             []RequestStep                `json:"steps,omitempty"`
	Diagnostics       []types.ConversionDiagnostic `json:"diagnostics,omitempty"`
}

// AnalyzeRequest reads protocol requirements without resolving media or using
// host state. The caller retains the original DTO for same-protocol forwarding.
func AnalyzeRequest(request any) (RequestRequirements, error) {
	format, err := inferRequestRelayFormat(request)
	if err != nil {
		return RequestRequirements{}, err
	}
	data, err := kitutil.Marshal(request)
	if err != nil {
		return RequestRequirements{}, err
	}
	var payload map[string]any
	if err := kitutil.Unmarshal(data, &payload); err != nil {
		return RequestRequirements{}, err
	}
	req := RequestRequirements{Format: format, Operation: "text"}
	if format == types.RelayFormatOpenAIResponses && payload["instructions"] != nil {
		if _, ok := payload["instructions"].(string); !ok {
			return req, fmt.Errorf("instructions must be a string or null")
		}
	}
	if format == types.RelayFormatOpenAI {
		req.Shape, err = analyzeChatRequestShape(payload)
		if err != nil {
			return req, err
		}
	}
	req.Model, _ = payload["model"].(string)
	req.Stream, _ = payload["stream"].(bool)
	if format == types.RelayFormatOpenAI {
		if count, ok := payload["n"].(float64); ok && count > 1 {
			req.Features = appendUnique(req.Features, "multiple_candidates")
		}
	}
	if format == types.RelayFormatGemini {
		if config, ok := payload["generationConfig"].(map[string]any); ok {
			if count, ok := config["candidateCount"].(float64); ok && count > 1 {
				req.Features = appendUnique(req.Features, "multiple_candidates")
			}
		}
	}
	switch format {
	case types.RelayFormatEmbedding:
		req.Operation = "embedding"
	case types.RelayFormatRerank:
		req.Operation = "rerank"
	case types.RelayFormatOpenAIImage:
		req.Operation = "image"
	case types.RelayFormatOpenAIAudio:
		req.Operation = "audio"
		req.Stream = req.Stream || payload["stream_format"] == "sse"
	case types.RelayFormatOpenAIResponsesCompaction:
		req.Operation = "compact"
		req.NativeState = append(req.NativeState, "compaction")
	}
	for _, name := range []string{"previous_response_id", "conversation", "prompt", "context_management", "cachedContent", "container", "mcp_servers"} {
		if valuePresent(payload[name]) && (name != "prompt" || format == types.RelayFormatOpenAIResponses) {
			req.NativeState = append(req.NativeState, name)
		}
	}
	for _, name := range []string{"messages", "input", "contents", "system", "systemInstruction"} {
		req.inspectContent(payload[name], name)
	}
	for _, name := range []string{"reasoning", "reasoning_effort", "thinking", "thinking_budget"} {
		if valuePresent(payload[name]) {
			req.Features = appendUnique(req.Features, "reasoning")
		}
	}
	if value, ok := payload["parallel_tool_calls"].(bool); ok && !value {
		req.Features = appendUnique(req.Features, "serial_tools")
	}
	if valuePresent(payload["functions"]) || valuePresent(payload["function_call"]) {
		req.Features = appendUnique(req.Features, "legacy_functions")
	}
	switch choice := payload["tool_choice"].(type) {
	case string:
		if choice != "auto" && choice != "none" {
			req.Features = appendUnique(req.Features, "forced_tools")
		}
	case map[string]any:
		if choice["type"] != "auto" && choice["type"] != "none" {
			req.Features = appendUnique(req.Features, "forced_tools")
		}
		if disabled, _ := choice["disable_parallel_tool_use"].(bool); disabled {
			req.Features = appendUnique(req.Features, "serial_tools")
		}
	default:
		if valuePresent(choice) {
			req.Features = appendUnique(req.Features, "forced_tools")
		}
	}
	if toolConfig, ok := payload["toolConfig"].(map[string]any); ok {
		if calling, ok := toolConfig["functionCallingConfig"].(map[string]any); ok && calling["mode"] == "ANY" {
			req.Features = appendUnique(req.Features, "forced_tools")
		}
	}
	for _, name := range []string{"metadata", "user", "client_metadata"} {
		if valuePresent(payload[name]) {
			req.Diagnostics = append(req.Diagnostics, types.ConversionDiagnostic{Code: "metadata_not_portable", Path: name, Message: "provider metadata may not survive protocol conversion", Severity: types.ConversionDiagnosticWarning})
		}
	}
	if err := req.inspectTools(payload); err != nil {
		return RequestRequirements{}, err
	}
	if err := req.inspectRetainedRequestFields(request, payload); err != nil {
		return RequestRequirements{}, err
	}
	if err := req.inspectProjectionSemantics(payload); err != nil {
		return RequestRequirements{}, err
	}
	req.NativeRequired = len(req.NativeState) != 0
	req.ToolIdentity = analyzeToolIdentity(request)
	return req, nil
}

// PlanConversion fixes both directions before a candidate is admitted. It does
// not claim that the host has implemented the corresponding provider adaptor.
func PlanConversion(req RequestRequirements, target types.RelayFormat, policy types.ConversionLossPolicy) (ConversionPlan, error) {
	if policy == "" {
		policy = types.ConversionLossPolicySafe
	}
	plan := ConversionPlan{From: req.Format, To: target, Native: req.Format == target, Policy: policy}
	if err := policy.Validate(); err != nil {
		return plan, err
	}
	if req.Format == "" || target == "" {
		return plan, fmt.Errorf("source and target relay formats are required")
	}
	if slices.Contains(req.Features, "multiple_candidates") &&
		(target == types.RelayFormatClaude || target == types.RelayFormatOpenAIResponses) {
		plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{
			Code: "multiple_candidates_unsupported", Path: "candidate_count",
			Message:  "the target response protocol cannot represent multiple answer candidates",
			Severity: types.ConversionDiagnosticError, From: req.Format, To: target,
		})
		return plan, &types.ConversionLossError{Diagnostics: plan.Diagnostics}
	}
	if plan.Native {
		plan.StreamSupported = true
		for _, diagnostic := range req.Diagnostics {
			if diagnostic.Code == "unknown_request_field" {
				diagnostic.From, diagnostic.To = req.Format, target
				plan.Diagnostics = append(plan.Diagnostics, diagnostic)
			}
		}
		return plan, types.RejectConversionLoss(policy, plan.Diagnostics)
	}
	if req.NativeRequired {
		plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{Code: "native_state_required", Path: strings.Join(req.NativeState, ","), Message: "provider-owned state requires the original protocol and owner", Severity: types.ConversionDiagnosticError, From: req.Format, To: target})
	}
	mediaDiagnostics, mediaErr := planMediaConversion(req, target)
	plan.Diagnostics = append(plan.Diagnostics, mediaDiagnostics...)
	if mediaErr != nil {
		return plan, mediaErr
	}
	requestSpec, ok := lookupRequestRoute(req.Format, target)
	if !ok {
		return plan, fmt.Errorf("request converter from %s to %s is not registered", req.Format, target)
	}
	responseSpec, ok := lookupResponseRoute(target, req.Format)
	if !ok {
		return plan, fmt.Errorf("response converter from %s to %s is not registered", target, req.Format)
	}
	plan.RequestConverter, plan.ResponseConverter = requestSpec.ID, responseSpec.ID
	steps, err := expandRequestConverterSteps(requestSpec)
	if err != nil {
		return plan, err
	}
	for _, step := range steps {
		plan.Steps = append(plan.Steps, RequestStep{Converter: step.ID, From: step.From, To: step.To})
	}
	responseSteps, err := expandResponseConverterSteps(responseSpec)
	if err != nil {
		return plan, err
	}
	plan.StreamSupported = true
	for _, step := range responseSteps {
		if step.Convert == nil {
			return plan, fmt.Errorf("response converter %q has no non-stream implementation", step.ID)
		}
		plan.StreamSupported = plan.StreamSupported && (step.ConvertStreamChunk != nil || step.ConvertStream != nil)
	}
	if req.Stream && !plan.StreamSupported {
		return plan, fmt.Errorf("stream response conversion from %s to %s is unavailable", target, req.Format)
	}
	for _, tool := range req.Tools {
		if tool.Type == "function" {
			continue
		}
		// Only unconfigured web search has a portable representation in this
		// release. Provider-specific filters must not be silently discarded.
		if tool.Type == "web_search" && !tool.Configured && (target == types.RelayFormatGemini || target == types.RelayFormatClaude || target == types.RelayFormatOpenAIResponses || target == types.RelayFormatOpenAI) {
			continue
		}
		plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{Code: "unsupported_tool_semantics", Path: tool.Path, Message: "cannot preserve tool " + tool.Type + " and its configuration", Severity: types.ConversionDiagnosticError, From: req.Format, To: target})
	}
	if target == types.RelayFormatGemini && slices.Contains(req.Features, "serial_tools") {
		plan.Diagnostics = append(plan.Diagnostics, types.ConversionDiagnostic{Code: "serial_tools_not_portable", Path: "parallel_tool_calls", Message: "Gemini conversion cannot enforce serial tool execution", Severity: types.ConversionDiagnosticError, From: req.Format, To: target})
	}
	for _, diagnostic := range req.Diagnostics {
		if diagnostic.To != "" && diagnostic.To != target {
			continue
		}
		if diagnostic.Code == "metadata_not_portable" && diagnostic.Path != "client_metadata" &&
			((req.Format == types.RelayFormatOpenAI && target == types.RelayFormatOpenAIResponses) || (req.Format == types.RelayFormatOpenAIResponses && target == types.RelayFormatOpenAI)) {
			continue
		}
		diagnostic.From, diagnostic.To = req.Format, target
		plan.Diagnostics = append(plan.Diagnostics, diagnostic)
	}
	return plan, types.RejectConversionLoss(policy, plan.Diagnostics)
}

// ExecuteConversionPlan verifies the concrete DTO still satisfies the selected
// plan. It never silently chooses a different converter or weaker policy.
func ExecuteConversionPlan(ctx context.Context, meta convmeta.Meta, plan ConversionPlan, request any) (*RequestResult, error) {
	if err := types.RejectConversionLoss(plan.Policy, plan.Diagnostics); err != nil {
		return &RequestResult{Diagnostics: plan.Diagnostics}, err
	}
	requirements, err := AnalyzeRequest(request)
	if err != nil {
		return nil, err
	}
	current, err := PlanConversion(requirements, plan.To, plan.Policy)
	if err != nil {
		return &RequestResult{Diagnostics: current.Diagnostics}, err
	}
	if current.From != plan.From || current.Native != plan.Native || current.RequestConverter != plan.RequestConverter || current.ResponseConverter != plan.ResponseConverter {
		return nil, fmt.Errorf("conversion plan no longer matches the request")
	}
	if plan.Native {
		return &RequestResult{Value: request, From: plan.From, To: plan.To, Diagnostics: plan.Diagnostics}, nil
	}
	options := *convmeta.OptionsOf(meta)
	options.ConversionPolicy = plan.Policy
	wrapped := &plannedMeta{Meta: meta, options: options}
	if meta == nil {
		wrapped.Meta = &convmeta.Values{}
	}
	result, err := ConvertRequestByID(ctx, wrapped, plan.RequestConverter, request)
	if result != nil {
		for _, diagnostic := range plan.Diagnostics {
			if !slices.Contains(result.Diagnostics, diagnostic) {
				result.Diagnostics = append(result.Diagnostics, diagnostic)
			}
		}
	}
	return result, err
}

type plannedMeta struct {
	convmeta.Meta
	options convmeta.Options
}

func (m *plannedMeta) ConvOptions() *convmeta.Options { return &m.options }

func (r *RequestRequirements) inspectTools(payload map[string]any) error {
	if valuePresent(payload["web_search_options"]) {
		options, _ := payload["web_search_options"].(map[string]any)
		configured := false
		for _, value := range options {
			configured = configured || valuePresent(value)
		}
		r.Tools = append(r.Tools, ToolRequirement{Type: "web_search", Path: "web_search_options", Configured: configured})
	}
	value := payload["tools"]
	if !valuePresent(value) {
		return nil
	}
	tools, ok := value.([]any)
	if !ok {
		return fmt.Errorf("tools must be an array")
	}
	for index, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("tools[%d] must be an object", index)
		}
		path := fmt.Sprintf("tools[%d]", index)
		if r.Format != types.RelayFormatGemini {
			definition := tool
			if nested, ok := tool["function"].(map[string]any); ok {
				definition = nested
			}
			for _, field := range []string{"defer_loading", "allowed_callers"} {
				if valuePresent(tool[field]) || valuePresent(definition[field]) {
					r.projectionLoss("unsupported_tool_semantics", path+"."+field, "conversion does not preserve deferred loading or tool caller restrictions", "")
				}
			}
			if strict, ok := definition["strict"].(bool); ok {
				severity := types.ConversionDiagnosticWarning
				if strict {
					severity = types.ConversionDiagnosticError
					r.Features = appendUnique(r.Features, "strict_tools")
				}
				r.Diagnostics = append(r.Diagnostics, types.ConversionDiagnostic{Code: "strict_tools_not_portable", Path: path + ".strict", Message: "Gemini conversion cannot preserve the explicit tool schema strictness setting", Severity: severity, To: types.RelayFormatGemini})
			}
			parameters := definition["parameters"]
			if r.Format == types.RelayFormatClaude {
				parameters = definition["input_schema"]
			}
			if err := r.inspectToolSchema(parameters, path); err != nil {
				return err
			}
		}
		if r.Format == types.RelayFormatGemini {
			names := make([]string, 0, len(tool))
			for name := range tool {
				names = append(names, name)
			}
			slices.Sort(names)
			for _, name := range names {
				config := tool[name]
				kind := name
				if name == "functionDeclarations" {
					kind = "function"
					declarations, _ := config.([]any)
					for index, value := range declarations {
						declaration, _ := value.(map[string]any)
						for _, field := range []string{"parametersJsonSchema", "response", "responseJsonSchema", "behavior"} {
							if valuePresent(declaration[field]) {
								r.projectionLoss("unsupported_tool_semantics", fmt.Sprintf("%s.%s[%d].%s", path, name, index, field), "conversion does not preserve this Gemini function constraint", "")
							}
						}
					}
				} else if name == "googleSearch" {
					kind = "web_search"
				}
				settings, _ := config.(map[string]any)
				r.Tools = append(r.Tools, ToolRequirement{Type: kind, Path: path + "." + name, Configured: len(settings) != 0})
			}
			continue
		}
		kind, _ := tool["type"].(string)
		if kind == "" && r.Format == types.RelayFormatClaude {
			kind = "function"
		}
		if kind == "web_search_preview" || strings.HasPrefix(kind, "web_search_") {
			kind = "web_search"
		}
		configured := false
		if kind == "web_search" {
			for field := range tool {
				if field != "type" && field != "name" {
					configured = true
				}
			}
		}
		r.Tools = append(r.Tools, ToolRequirement{Type: kind, Path: path, Configured: configured})
	}
	if len(r.Tools) != 0 {
		r.Features = appendUnique(r.Features, "tools")
	}
	return nil
}

func (r *RequestRequirements) inspectContent(value any, path string) {
	switch value := value.(type) {
	case []any:
		for index, item := range value {
			r.inspectContent(item, fmt.Sprintf("%s[%d]", path, index))
		}
	case map[string]any:
		r.inspectMedia(value, path)
		kind, _ := value["type"].(string)
		switch kind {
		case "image", "document", "audio", "video", "input_file", "file":
			r.Features = appendUnique(r.Features, "multimodal")
		case "compaction", "compaction_trigger", "item_reference", "redacted_thinking", "server_tool_use", "web_search_tool_result":
			r.NativeState = appendUnique(r.NativeState, path+"."+kind)
		case "custom_tool_call", "custom_tool_call_output":
			r.Tools = append(r.Tools, ToolRequirement{Type: "custom_history", Path: path})
		}
		if kind == "tool_result" {
			if isError, _ := value["is_error"].(bool); isError {
				r.projectionLoss("tool_result_status_not_portable", path+".is_error", "conversion does not preserve the failed tool execution status", "")
			}
		}
		if response, ok := value["functionResponse"].(map[string]any); ok {
			for _, field := range []string{"willContinue", "scheduling", "parts"} {
				if valuePresent(response[field]) {
					r.projectionLoss("tool_result_status_not_portable", path+".functionResponse."+field, "conversion does not preserve Gemini tool result continuation or media", "")
				}
			}
		}
		for _, field := range []string{"encrypted_content", "thoughtSignature", "signature", "file_id"} {
			if valuePresent(value[field]) {
				r.NativeState = appendUnique(r.NativeState, path+"."+field)
			}
		}
		for _, field := range []string{"image_url", "input_image", "inlineData", "fileData", "input_audio", "video_url"} {
			if valuePresent(value[field]) || kind == field {
				r.Features = appendUnique(r.Features, "multimodal")
			}
		}
		for _, field := range []string{"content", "parts", "source", "image_url", "file", "input_audio", "tool_calls", "function"} {
			if nested, ok := value[field]; ok {
				r.inspectContent(nested, path+"."+field)
			}
		}
	}
}

func valuePresent(value any) bool {
	if value == nil || value == "" {
		return false
	}
	return true
}

func appendUnique(values []string, value string) []string {
	if !slices.Contains(values, value) {
		return append(values, value)
	}
	return values
}
