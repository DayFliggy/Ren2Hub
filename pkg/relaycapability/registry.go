// Package relaycapability describes the host adaptor surfaces without importing
// transport implementations. Relay binds each description to its constructor.
package relaycapability

import (
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type Surface struct {
	Incoming        types.RelayFormat
	Target          types.RelayFormat
	Native          bool
	Stream          bool
	RequestPaths    []string
	UnboundedOutput bool
	BudgetCodec     string
	TextProjection  *TextProjection
}

type Definition struct {
	APIType   int
	Surfaces  []Surface
	Endpoints []constant.EndpointType
}

func (s Surface) SupportsTokenBudget() bool {
	return !s.UnboundedOutput && s.BudgetCodec != ""
}

func (s Surface) SupportsPath(path string) bool {
	return len(s.RequestPaths) == 0 || slices.Contains(s.RequestPaths, strings.Split(path, "?")[0])
}

// Reference domains are narrower than wire compatibility: a proxy implementing
// Responses does not thereby own OpenAI dashboard prompt resources.
func SupportsReferenceDomain(channelType int, format types.RelayFormat, domain string) bool {
	return domain == "openai_prompt" && channelType == constant.ChannelTypeOpenAI && format == types.RelayFormatOpenAIResponses
}

func (d Definition) Resolve(format types.RelayFormat) (Surface, bool) {
	for _, surface := range d.Surfaces {
		if surface.Incoming == format {
			return surface, true
		}
	}
	return Surface{}, false
}

func (d Definition) EndpointTypes() []constant.EndpointType {
	result := append([]constant.EndpointType(nil), d.Endpoints...)
	for _, surface := range d.Surfaces {
		endpoint := EndpointForFormat(surface.Incoming)
		if endpoint != "" && !slices.Contains(result, endpoint) {
			result = append(result, endpoint)
		}
	}
	return result
}

func EndpointForFormat(format types.RelayFormat) constant.EndpointType {
	switch format {
	case types.RelayFormatOpenAI:
		return constant.EndpointTypeOpenAI
	case types.RelayFormatClaude:
		return constant.EndpointTypeAnthropic
	case types.RelayFormatGemini:
		return constant.EndpointTypeGemini
	case types.RelayFormatOpenAIResponses:
		return constant.EndpointTypeOpenAIResponse
	case types.RelayFormatOpenAIResponsesCompaction:
		return constant.EndpointTypeOpenAIResponseCompact
	case types.RelayFormatEmbedding:
		return constant.EndpointTypeEmbeddings
	case types.RelayFormatRerank:
		return constant.EndpointTypeJinaRerank
	case types.RelayFormatOpenAIAudio:
		return constant.EndpointTypeAudio
	case types.RelayFormatOpenAIImage:
		return constant.EndpointTypeImageGeneration
	case types.RelayFormatOpenAIRealtime:
		return constant.EndpointTypeRealtime
	case types.RelayFormatOpenAIAlphaSearch:
		return constant.EndpointTypeOpenAIAlphaSearch
	default:
		return ""
	}
}

// Describe lists implemented request and response entry points. A converter
// existing in RelayKit alone never adds a host surface here.
func Describe(apiType int, model string) (Definition, bool) {
	d := Definition{APIType: apiType}
	chat, claude, gemini, responses := types.RelayFormatOpenAI, types.RelayFormat(types.RelayFormatClaude), types.RelayFormat(types.RelayFormatGemini), types.RelayFormat(types.RelayFormatOpenAIResponses)
	add := func(in, target types.RelayFormat, native bool) {
		d.Surfaces = append(d.Surfaces, Surface{Incoming: in, Target: target, Native: native, Stream: true})
	}
	addAll := func(target types.RelayFormat) {
		for _, source := range []types.RelayFormat{chat, claude, gemini, responses} {
			add(source, target, source == target)
		}
	}
	media := func(formats ...types.RelayFormat) {
		for _, format := range formats {
			d.Surfaces = append(d.Surfaces, Surface{Incoming: format, Target: format, Native: true})
		}
	}
	switch apiType {
	case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference:
		addAll(chat)
		for i := range d.Surfaces {
			if d.Surfaces[i].Incoming == responses {
				d.Surfaces[i].Target, d.Surfaces[i].Native = responses, true
			}
		}
		media(types.RelayFormatEmbedding, types.RelayFormatRerank, types.RelayFormatOpenAIAudio, types.RelayFormatOpenAIImage, types.RelayFormatOpenAIRealtime)
		if apiType == constant.APITypeOpenAI {
			media(types.RelayFormatOpenAIResponsesCompaction)
		}
	case constant.APITypeAnthropic:
		addAll(claude)
	case constant.APITypeAws:
		if strings.Contains(model, "nova-") {
			d.Surfaces = append(d.Surfaces, Surface{Incoming: chat, Target: chat, TextProjection: &TextProjection{
				ID: "aws_nova_text", Roles: []string{"system", "user", "assistant"},
				Fields: []string{"model", "messages", "stream", "max_tokens", "max_completion_tokens", "temperature", "top_p", "top_k", "stop"},
			}})
		} else {
			addAll(claude)
		}
	case constant.APITypeGemini:
		addAll(gemini)
		media(types.RelayFormatEmbedding, types.RelayFormatOpenAIImage)
	case constant.APITypeVertexAi:
		if strings.HasPrefix(model, "claude") {
			addAll(claude)
		} else if IsVertexOpenSourceModel(model) {
			add(chat, chat, true)
			add(gemini, chat, false)
			add(responses, chat, false)
		} else {
			addAll(gemini)
			media(types.RelayFormatOpenAIImage)
		}
	case constant.APITypeNewAPI, constant.APITypeSub2API:
		for _, format := range []types.RelayFormat{chat, claude, gemini, responses} {
			add(format, format, true)
		}
		media(types.RelayFormatOpenAIResponsesCompaction, types.RelayFormatOpenAIAlphaSearch, types.RelayFormatEmbedding, types.RelayFormatOpenAIImage)
	case constant.APITypeCodex:
		add(responses, responses, true)
		d.Surfaces[0].UnboundedOutput = true
		media(types.RelayFormatOpenAIResponsesCompaction, types.RelayFormatOpenAIAlphaSearch)
	case constant.APITypeAdvancedCustom:
		// Configured routes supply the actual surfaces and converter identifiers.
	case constant.APITypeDeepSeek:
		add(chat, chat, true)
		add(claude, claude, true)
		add(responses, responses, true)
	case constant.APITypeAli:
		add(chat, chat, true)
		add(claude, chat, false)
		add(responses, responses, true)
		media(types.RelayFormatEmbedding, types.RelayFormatRerank, types.RelayFormatOpenAIImage)
	case constant.APITypeMoonshot, constant.APITypeMiniMax:
		add(chat, chat, true)
		add(claude, claude, true)
		if apiType == constant.APITypeMoonshot {
			media(types.RelayFormatEmbedding, types.RelayFormatRerank)
		} else {
			media(types.RelayFormatEmbedding, types.RelayFormatOpenAIImage, types.RelayFormatOpenAIAudio)
		}
	case constant.APITypeSiliconFlow:
		add(chat, chat, true)
		add(claude, chat, false)
		media(types.RelayFormatEmbedding, types.RelayFormatRerank, types.RelayFormatOpenAIAudio, types.RelayFormatOpenAIImage)
	case constant.APITypeVolcEngine:
		add(chat, chat, true)
		add(claude, chat, false)
		add(responses, responses, true)
		media(types.RelayFormatEmbedding, types.RelayFormatOpenAIAudio, types.RelayFormatOpenAIImage)
	case constant.APITypePerplexity:
		add(chat, chat, true)
		add(claude, chat, false)
		add(responses, responses, true)
	case constant.APITypeOllama, constant.APITypeBaiduV2:
		add(chat, chat, false)
		add(claude, chat, false)
		if apiType == constant.APITypeOllama {
			add(responses, chat, false)
			add(gemini, chat, false)
			for index := range d.Surfaces {
				d.Surfaces[index].TextProjection = OllamaChatProjection()
			}
			media(types.RelayFormatEmbedding)
		}
	case constant.APITypeCloudflare:
		add(chat, chat, false)
		add(responses, responses, true)
		media(types.RelayFormatEmbedding, types.RelayFormatOpenAIAudio)
	case constant.APITypeXai:
		add(chat, chat, true)
		add(responses, responses, true)
		media(types.RelayFormatOpenAIImage)
	case constant.APITypeJina:
		media(types.RelayFormatEmbedding, types.RelayFormatRerank)
	case constant.APITypeMokaAI:
		media(types.RelayFormatEmbedding)
	case constant.APITypeZhipuV4:
		add(chat, chat, false)
		add(claude, claude, true)
		media(types.RelayFormatEmbedding, types.RelayFormatOpenAIImage)
	case constant.APITypeCohere:
		add(chat, chat, false)
		media(types.RelayFormatRerank)
	case constant.APITypeReplicate, constant.APITypeJimeng:
		media(types.RelayFormatOpenAIImage)
	case constant.APITypeCoze:
		add(chat, chat, false)
		d.Surfaces[0].TextProjection = &TextProjection{ID: "coze_text", Roles: []string{"user", "assistant"}, Fields: []string{"model", "messages", "stream", "stream_options", "user"}}
	case constant.APITypeBaidu, constant.APITypeZhipu, constant.APITypePaLM, constant.APITypeTencent, constant.APITypeXunfei, constant.APITypeDify, constant.APITypeMistral, constant.APITypeSubmodel:
		add(chat, chat, false)
		if apiType == constant.APITypeBaidu {
			media(types.RelayFormatEmbedding)
		}
	default:
		return d, false
	}
	switch apiType {
	case constant.APITypeBaidu:
		d.Surfaces[0].TextProjection = &TextProjection{ID: "baidu_text", Roles: []string{"system", "user", "assistant"}, SystemAtStart: true,
			Fields: []string{"model", "messages", "stream", "stream_options", "max_tokens", "max_completion_tokens", "temperature", "top_p", "frequency_penalty", "user"}}
	case constant.APITypeCohere:
		d.Surfaces[0].TextProjection = &TextProjection{ID: "cohere_text", Roles: []string{"system", "user", "assistant"}, LastRole: "user",
			Fields: []string{"model", "messages", "stream", "stream_options", "max_tokens", "max_completion_tokens"}}
	case constant.APITypeZhipu:
		d.Surfaces[0].TextProjection = &TextProjection{ID: "zhipu_text", Roles: []string{"system", "user", "assistant"},
			Fields: []string{"model", "messages", "stream", "stream_options", "temperature", "top_p"}}
	case constant.APITypeTencent:
		d.Surfaces[0].TextProjection = &TextProjection{ID: "tencent_tc3_text", Roles: []string{"system", "user", "assistant"}, SystemAtStart: true, LastRole: "user",
			Fields: []string{"model", "messages", "stream", "stream_options", "temperature", "top_p"}}
	case constant.APITypeXunfei:
		roles := []string{"user", "assistant"}
		if strings.HasSuffix(model, "3.5") {
			roles = append(roles, "system")
		}
		d.Surfaces[0].TextProjection = &TextProjection{ID: "xunfei_text", Roles: roles, SystemAtStart: true, LastRole: "user",
			Fields: []string{"model", "messages", "stream", "stream_options", "max_tokens", "max_completion_tokens", "temperature", "top_k"}}
	case constant.APITypePaLM:
		d.Surfaces[0].TextProjection = &TextProjection{ID: "palm_text", Roles: []string{"user", "assistant"}, LastRole: "user",
			Fields: []string{"model", "messages", "stream", "stream_options", "temperature", "top_p", "top_k"}}
	case constant.APITypeDify:
		d.Surfaces[0].TextProjection = &TextProjection{ID: "dify_prompt", Roles: []string{"user"},
			Fields: []string{"model", "messages", "stream", "stream_options", "user"}, Images: true, SingleTurn: true}
	}
	// A protocol name alone does not establish the provider's final wire schema.
	// Legacy projections and media envelopes need their own budget decoders.
	for i := range d.Surfaces {
		s := &d.Surfaces[i]
		if s.Incoming == types.RelayFormatOpenAIAudio {
			s.Stream = apiType != constant.APITypeCloudflare
			switch apiType {
			case constant.APITypeCloudflare:
				s.RequestPaths = []string{"/v1/audio/transcriptions"}
			case constant.APITypeMiniMax, constant.APITypeVolcEngine:
				s.RequestPaths = []string{"/v1/audio/speech"}
			}
		}
		switch apiType {
		case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
			constant.APITypeAnthropic, constant.APITypeGemini, constant.APITypeVertexAi,
			constant.APITypeNewAPI, constant.APITypeSub2API, constant.APITypeDeepSeek,
			constant.APITypeAli, constant.APITypeMoonshot, constant.APITypeMiniMax,
			constant.APITypeSiliconFlow, constant.APITypeVolcEngine, constant.APITypePerplexity, constant.APITypeXai:
			switch s.Target {
			case chat, claude, gemini, responses:
				s.BudgetCodec = string(s.Target)
			}
		case constant.APITypeAws:
			s.BudgetCodec = string(claude)
			if s.TextProjection != nil {
				s.BudgetCodec = "aws_nova"
			}
		case constant.APITypeZhipuV4:
			if s.Target == claude {
				s.BudgetCodec = string(claude)
			}
		}
		if s.Incoming == types.RelayFormatEmbedding {
			switch apiType {
			case constant.APITypeOpenAI, constant.APITypeOpenRouter, constant.APITypeXinference,
				constant.APITypeNewAPI, constant.APITypeSub2API, constant.APITypeSiliconFlow, constant.APITypeJina, constant.APITypeMokaAI:
				s.BudgetCodec = string(types.RelayFormatEmbedding)
			}
		}
	}
	return d, true
}

func IsVertexOpenSourceModel(model string) bool {
	return strings.Contains(model, "llama") || strings.Contains(model, "-maas")
}

// DescribeForCredentials projects credential-dependent executable surfaces.
// Credential values are inspected only for dispatch shape and never retained.
func DescribeForCredentials(apiType int, model string, credentials []string, settings ...dto.ChannelOtherSettings) (Definition, bool) {
	definition, found := Describe(apiType, model)
	if found && apiType == constant.APITypeVertexAi && IsVertexOpenSourceModel(model) &&
		len(settings) > 0 && settings[0].VertexKeyType == dto.VertexKeyTypeAPIKey {
		definition.Surfaces, definition.Endpoints = nil, nil
		return definition, true
	}
	if !found || apiType != constant.APITypeTencent || len(credentials) == 0 {
		return definition, found
	}
	for _, credential := range credentials {
		if strings.TrimSpace(credential) == "" || strings.Contains(credential, "|") {
			return definition, found
		}
	}
	definition.Surfaces[0].Native = true
	definition.Surfaces[0].TextProjection = nil
	return definition, found
}
