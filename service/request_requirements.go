package service

import (
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// RequestRequirements is the single description of what a request needs from a
// channel. It is derived once per request and shared by candidate filtering,
// the manual preview and conversion planning, so those three cannot disagree
// about whether a candidate is eligible.
//
// Before this type existed the request path checked the endpoint type, native
// Responses support and Compact stage support, while the preview checked only
// the endpoint type. A preview therefore reported candidates as usable that the
// request path would reject.
type RequestRequirements struct {
	RequestModel string
	RequestPath  string
	// EndpointType is the capability bucket the path maps to. An empty value
	// means the path is not routable, which the shared filter rejects.
	EndpointType string
	// NativeResponsesRequired marks Responses requests carrying opaque
	// server-side compaction state, which cannot survive a converter pivot.
	NativeResponsesRequired bool
	CompactStage            relaycommon.CompactAttemptStage
	// CompactBaseModel is the base model a Compact request resolves to. It is
	// empty outside Compact so callers cannot accidentally apply Compact rules.
	CompactBaseModel string
	StreamMode       bool
	InputModalities  []string
	ToolTypes        []string
	StructuredOutput bool
	Reasoning        bool
	Continuation     bool
}

// RequestRequirementFeatures are facts extracted from the replayable request
// body once at the routing boundary. Conversion and candidate qualification can
// inspect them without parsing the body again.
type RequestRequirementFeatures struct {
	InputModalities  []string
	ToolTypes        []string
	StructuredOutput bool
	Reasoning        bool
	Continuation     bool
}

// RequestRequirementFeaturesFromJSON extracts routing-relevant facts from the
// replayable client body without changing validation semantics. Invalid JSON
// returns an empty feature set; the normal request decoder remains responsible
// for reporting the client error.
func RequestRequirementFeaturesFromJSON(data []byte) RequestRequirementFeatures {
	var root any
	if len(data) == 0 || common.Unmarshal(data, &root) != nil {
		return RequestRequirementFeatures{}
	}

	modalities := make(map[string]struct{})
	toolTypes := make(map[string]struct{})
	features := RequestRequirementFeatures{}

	rootObject, ok := root.(map[string]any)
	if !ok {
		return features
	}

	var walkContent func(value any, key string)
	walkContent = func(value any, key string) {
		switch typed := value.(type) {
		case []any:
			for _, item := range typed {
				walkContent(item, key)
			}
		case map[string]any:
			if contentType := normalizedRequirementString(typed["type"]); contentType != "" {
				if modality := requestModalityForContentType(contentType); modality != "" {
					modalities[modality] = struct{}{}
				}
				if contentType == "compaction" || contentType == "item_reference" {
					features.Continuation = true
				}
			}
			for childKey, child := range typed {
				normalizedKey := normalizeRequirementKey(childKey)
				switch normalizedKey {
				case "previousresponseid", "conversation", "contextmanagement", "container":
					if requirementValuePresent(child) {
						features.Continuation = true
					}
				case "mimetype":
					if modality := requestModalityForMIME(normalizedRequirementString(child)); modality != "" {
						modalities[modality] = struct{}{}
					}
				case "text", "prompt", "instructions":
					if requirementValuePresent(child) {
						modalities["text"] = struct{}{}
					}
				}
				walkContent(child, childKey)
			}
		case string:
			switch normalizeRequirementKey(key) {
			case "input", "prompt", "instructions", "text", "content":
				if strings.TrimSpace(typed) != "" {
					modalities["text"] = struct{}{}
				}
			}
		}
	}

	for key, value := range rootObject {
		normalizedKey := normalizeRequirementKey(key)
		if normalizedKey == "tools" {
			for _, tool := range requirementArray(value) {
				definition, ok := tool.(map[string]any)
				if !ok {
					continue
				}
				toolType := normalizedRequirementString(definition["type"])
				if toolType == "" {
					switch {
					case definition["function"] != nil || definition["functionDeclarations"] != nil || definition["function_declarations"] != nil:
						toolType = "function"
					case definition["web_search"] != nil || definition["webSearch"] != nil || definition["googleSearch"] != nil || definition["google_search"] != nil:
						toolType = "web_search"
					case definition["codeExecution"] != nil || definition["code_execution"] != nil:
						toolType = "code_execution"
					}
				}
				if toolType != "" {
					toolTypes[toolType] = struct{}{}
				}
			}
			continue
		}

		switch normalizedKey {
		case "previousresponseid", "conversation", "contextmanagement", "container":
			features.Continuation = features.Continuation || requirementValuePresent(value)
		case "reasoning", "reasoningeffort", "thinking":
			features.Reasoning = features.Reasoning || requirementValuePresent(value)
		case "responseformat", "outputformat":
			features.StructuredOutput = features.StructuredOutput || structuredRequirementValue(value)
		case "text", "outputconfig":
			if object, ok := value.(map[string]any); ok && structuredRequirementValue(object["format"]) {
				features.StructuredOutput = true
			}
		case "generationconfig":
			if config, ok := value.(map[string]any); ok {
				if strings.Contains(normalizedRequirementString(firstRequirementValue(config, "responseMimeType", "response_mime_type")), "json") ||
					requirementValuePresent(firstRequirementValue(config, "responseSchema", "response_schema", "responseJsonSchema", "response_json_schema")) {
					features.StructuredOutput = true
				}
				features.Reasoning = features.Reasoning || requirementValuePresent(firstRequirementValue(config, "thinkingConfig", "thinking_config"))
			}
		}
		walkContent(value, key)
	}

	features.InputModalities = sortedRequirementSet(modalities)
	features.ToolTypes = sortedRequirementSet(toolTypes)
	return features
}

func requirementArray(value any) []any {
	items, _ := value.([]any)
	return items
}

func firstRequirementValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func normalizeRequirementKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("_", "", "-", "").Replace(value)
}

func normalizedRequirementString(value any) string {
	text, _ := value.(string)
	return strings.ToLower(strings.TrimSpace(text))
}

func requirementValuePresent(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

func structuredRequirementValue(value any) bool {
	switch typed := value.(type) {
	case string:
		typeName := strings.ToLower(strings.TrimSpace(typed))
		return typeName != "" && typeName != "text"
	case map[string]any:
		typeName := normalizedRequirementString(typed["type"])
		return typeName != "" && typeName != "text"
	default:
		return false
	}
}

func requestModalityForContentType(contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(contentType, "image"):
		return "image"
	case strings.Contains(contentType, "audio"):
		return "audio"
	case strings.Contains(contentType, "video"):
		return "video"
	case strings.Contains(contentType, "file") || strings.Contains(contentType, "document"):
		return "document"
	case contentType == "text" || contentType == "input_text" || contentType == "output_text":
		return "text"
	default:
		return ""
	}
}

func requestModalityForMIME(mimeType string) string {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	case mimeType != "":
		return "document"
	default:
		return ""
	}
}

func sortedRequirementSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

// RouteEndpointTypeForPath exposes the endpoint-type derivation so callers
// outside this package can label an observation with the same protocol the
// selector scored with.
func RouteEndpointTypeForPath(requestPath string) string {
	return endpointTypeForRequestPath(requestPath)
}

// CompactStageForRequest derives the initial Compact stage from the request
// path and model, the same way the distributor does before its first attempt.
// A retry may narrow Exact to Base, so the request path passes its own stage
// rather than re-deriving it here.
func CompactStageForRequest(requestPath, requestModel string) relaycommon.CompactAttemptStage {
	if !strings.HasPrefix(requestPath, "/v1/responses/compact") {
		return relaycommon.CompactAttemptNone
	}
	if ratio_setting.IsGPTCompactBaseModel(ratio_setting.CompactBaseModelName(requestModel)) {
		return relaycommon.CompactAttemptExact
	}
	return relaycommon.CompactAttemptBase
}

// NewRequestRequirements derives the requirements for one request. Callers pass
// facts already established by auth and distribution; nothing here reads the
// request body or contacts a provider.
func NewRequestRequirements(requestModel, requestPath string, nativeResponsesRequired bool, stage relaycommon.CompactAttemptStage) RequestRequirements {
	return NewRequestRequirementsWithFeatures(requestModel, requestPath, nativeResponsesRequired, stage, RequestRequirementFeatures{})
}

func NewRequestRequirementsWithFeatures(requestModel, requestPath string, nativeResponsesRequired bool, stage relaycommon.CompactAttemptStage, features RequestRequirementFeatures) RequestRequirements {
	return NewRequestRequirementsWithStream(requestModel, requestPath, nativeResponsesRequired, stage, false, features)
}

func NewRequestRequirementsWithStream(requestModel, requestPath string, nativeResponsesRequired bool, stage relaycommon.CompactAttemptStage, streamMode bool, features RequestRequirementFeatures) RequestRequirements {
	requirements := RequestRequirements{
		RequestModel:            strings.TrimSpace(requestModel),
		RequestPath:             requestPath,
		EndpointType:            endpointTypeForRequestPath(requestPath),
		NativeResponsesRequired: nativeResponsesRequired,
		CompactStage:            stage,
		StreamMode:              streamMode,
		InputModalities:         append([]string(nil), features.InputModalities...),
		ToolTypes:               append([]string(nil), features.ToolTypes...),
		StructuredOutput:        features.StructuredOutput,
		Reasoning:               features.Reasoning,
		Continuation:            features.Continuation,
	}
	if stage != relaycommon.CompactAttemptNone {
		requirements.CompactBaseModel = ratio_setting.CompactBaseModelName(requirements.RequestModel)
	}
	return requirements
}

// CandidateCompatibilityTier orders eligible candidates by protocol fidelity.
// Tier zero keeps the client's wire protocol intact. Positive tiers require a
// compatibility conversion and are tried only after native routes.
func (r RequestRequirements) CandidateCompatibilityTier(channel *model.Channel) int {
	if channel == nil {
		return 1
	}
	if channel.Type == constant.ChannelTypeAdvancedCustom {
		config := channel.GetOtherSettings().AdvancedCustom
		if config == nil {
			return 1
		}
		route, ok := config.MatchPathForModel(r.RequestPath, r.RequestModel)
		if !ok {
			return 1
		}
		converter := strings.TrimSpace(route.Converter)
		if converter == "" || strings.EqualFold(converter, "none") {
			return 0
		}
		return 1
	}

	switch r.EndpointType {
	case string(constant.EndpointTypeAnthropic):
		if channel.Type == constant.ChannelTypeAnthropic || channel.Type == constant.ChannelTypeAws || channel.Type == constant.ChannelTypeNewAPI || channel.Type == constant.ChannelTypeSub2API {
			return 0
		}
	case string(constant.EndpointTypeGemini):
		if channel.Type == constant.ChannelTypeGemini || channel.Type == constant.ChannelTypeVertexAi || channel.Type == constant.ChannelTypeNewAPI || channel.Type == constant.ChannelTypeSub2API {
			return 0
		}
	case string(constant.EndpointTypeOpenAIResponse):
		if ChannelSupportsNativeResponses(channel, r.RequestModel) || channel.Type == constant.ChannelTypeXai {
			return 0
		}
	case string(constant.EndpointTypeOpenAI):
		apiType, ok := common.ChannelType2APIType(channel.Type)
		if ok && apiType != constant.APITypeAnthropic && apiType != constant.APITypeGemini && apiType != constant.APITypeVertexAi {
			return 0
		}
	default:
		return 0
	}
	return 1
}

// ChannelMeetsRequirements reports the filter reason for a channel that cannot
// serve these requirements, or an empty string when it can. It covers only the
// protocol and capability facts carried by RequestRequirements; snapshot,
// entitlement, token and price checks stay in filterRouteCapability.
//
// abilityModels is the channel's enabled ability models and is consulted only
// for Compact, where stage support depends on which model variants exist.
func (r RequestRequirements) ChannelMeetsRequirements(channel *model.Channel, abilityModels map[string]bool) string {
	if channel == nil {
		return RouteFilterChannelDisabled
	}
	if r.NativeResponsesRequired && !ChannelSupportsNativeResponses(channel, r.RequestModel) {
		return RouteFilterPathUnsupported
	}
	if r.CompactStage == relaycommon.CompactAttemptNone {
		return ""
	}
	base := r.CompactBaseModel
	if !compactChannelSupportsStage(channel, abilityModels, base, ratio_setting.WithCompactModelSuffix(base), r.CompactStage) {
		return RouteFilterPathUnsupported
	}
	return ""
}

// CandidateLookupModels lists the request models to search in the capability
// index, in priority order. Compact resolves its exact variant before the base
// model; every other protocol looks up exactly one model.
func (r RequestRequirements) CandidateLookupModels(normalize func(string) string) []string {
	if r.CompactStage == relaycommon.CompactAttemptNone {
		return []string{normalize(r.RequestModel)}
	}
	base := normalize(r.CompactBaseModel)
	if r.CompactStage != relaycommon.CompactAttemptExact {
		return []string{base}
	}
	return []string{normalize(ratio_setting.WithCompactModelSuffix(r.CompactBaseModel)), base}
}
