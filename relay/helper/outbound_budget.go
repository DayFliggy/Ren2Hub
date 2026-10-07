package helper

import (
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/relaycapability"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/tokenkit"
)

// OutboundTokenBudget is an estimate of the serialized attempt, after all
// adaptor and channel overrides. It never makes a stateful/media input bounded.
type OutboundTokenBudget struct {
	InputTokens  int
	OutputTokens int
	InputKnown   bool
	Bounded      bool
	Reason       string
}

func AnalyzeOutboundTokenBudget(info *relaycommon.RelayInfo, body io.Reader) (*OutboundTokenBudget, error) {
	definition, _ := relaycapability.Describe(info.ApiType, info.UpstreamModelName)
	surface, _ := definition.Resolve(info.RelayFormat)
	if !surface.SupportsTokenBudget() || body == nil {
		return nil, nil
	}
	if surface.BudgetCodec != "aws_nova" {
		// Native endpoint switches (for example Ali Claude) and Chat-to-Responses
		// upgrades are finalized by the adaptor, after candidate planning.
		switch format := info.GetFinalRequestRelayFormat(); format {
		case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude, types.RelayFormatGemini:
			surface.BudgetCodec = string(format)
		}
	}
	var payload map[string]json.RawMessage
	if err := common.DecodeJson(body, &payload); err != nil {
		return nil, fmt.Errorf("invalid outbound request: %w", err)
	}
	if payload == nil {
		return nil, fmt.Errorf("outbound request must be a JSON object")
	}
	pricingPayload, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := FreezeOutboundBillingAttributes(info, pricingPayload); err != nil {
		return nil, err
	}
	var request dto.Request
	var inputFields, limitFields, countFields []string
	limits := payload
	embedding := false
	switch surface.BudgetCodec {
	case string(types.RelayFormatOpenAI):
		request = &dto.GeneralOpenAIRequest{}
		inputFields = []string{"messages", "prompt", "input", "instruction", "tools", "functions", "response_format", "prefix", "suffix"}
		limitFields, countFields = []string{"max_tokens", "max_completion_tokens"}, []string{"n", "best_of"}
	case string(types.RelayFormatOpenAIResponses):
		request = &dto.OpenAIResponsesRequest{}
		inputFields, limitFields = []string{"instructions", "input", "tools", "text"}, []string{"max_output_tokens"}
	case string(types.RelayFormatClaude):
		request = &dto.ClaudeRequest{}
		inputFields, limitFields = []string{"system", "messages", "tools", "output_config"}, []string{"max_tokens_to_sample", "max_tokens"}
	case string(types.RelayFormatGemini):
		request = &dto.GeminiChatRequest{}
		inputFields = []string{"contents", "systemInstruction", "system_instruction", "tools"}
		limits = nil
		if raw := payload["generationConfig"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &limits); err != nil {
				return nil, fmt.Errorf("invalid generationConfig: %w", err)
			}
		} else {
			limits = nil
		}
		limitFields, countFields = []string{"maxOutputTokens", "max_output_tokens"}, []string{"candidateCount", "candidate_count"}
	case "aws_nova":
		inputFields, limitFields = []string{"system", "messages"}, []string{"maxTokens"}
		limits = nil
		if raw := payload["inferenceConfig"]; len(raw) > 0 {
			if err := common.Unmarshal(raw, &limits); err != nil {
				return nil, fmt.Errorf("invalid inferenceConfig: %w", err)
			}
		} else {
			limits = nil
		}
	case string(types.RelayFormatEmbedding):
		inputFields, embedding = []string{"input"}, true
	default:
		return nil, nil
	}
	output, err := outboundBudgetQuantity(limits, limitFields, 0, false)
	if err != nil {
		return nil, err
	}
	count, err := outboundBudgetQuantity(limits, countFields, 1, true)
	if err != nil {
		return nil, err
	}
	budget := &OutboundTokenBudget{InputKnown: true, Bounded: embedding || output > 0, Reason: "serialized_input_estimate"}
	if !budget.Bounded {
		budget.Reason = "output_limit_missing"
		if count > 1 {
			output = defaultTieredPreConsumeMaxTokens
		}
	}
	if output > maxTokensLimit/count {
		return nil, fmt.Errorf("aggregate outbound token limit is invalid")
	}
	budget.OutputTokens = output * count
	input := make(map[string]json.RawMessage, len(inputFields))
	for _, name := range inputFields {
		if raw, ok := payload[name]; ok {
			input[name] = raw
		}
	}
	if surface.BudgetCodec == string(types.RelayFormatGemini) {
		for _, field := range []string{"responseSchema", "response_schema", "responseJsonSchema", "response_json_schema"} {
			if raw, ok := limits[field]; ok {
				input[field] = raw
			}
		}
	}
	if request != nil {
		encoded, err := common.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if err := common.Unmarshal(encoded, request); err != nil {
			return nil, fmt.Errorf("invalid outbound protocol fields: %w", err)
		}
		requirements, err := relayconvert.AnalyzeRequestJSON(request, encoded)
		if err != nil {
			return nil, err
		}
		if requirements.NativeRequired || slices.Contains(requirements.Features, "multimodal") || len(request.GetTokenCountMeta().Files) > 0 {
			budget.InputKnown, budget.Bounded, budget.Reason = false, false, "state_or_media_input_unbounded"
		}
		for _, diagnostic := range requirements.Diagnostics {
			if diagnostic.Code == "unknown_request_field" && diagnostic.Path != "anthropic_version" && diagnostic.Path != "anthropic_beta" {
				budget.Bounded, budget.Reason = false, "unknown_outbound_field"
			}
		}
	}
	if len(input) == 0 {
		budget.InputKnown, budget.Bounded, budget.Reason = false, false, "input_budget_unavailable"
	}
	for _, source := range []struct {
		fields map[string]json.RawMessage
		names  []string
	}{{payload, []string{"modalities"}}, {limits, []string{"responseModalities", "response_modalities"}}} {
		for _, name := range source.names {
			if raw := source.fields[name]; len(raw) > 0 {
				var modalities []string
				if err := common.Unmarshal(raw, &modalities); err != nil {
					return nil, fmt.Errorf("invalid outbound %s", name)
				}
				for _, modality := range modalities {
					if modality != "TEXT" && modality != "text" {
						budget.Bounded, budget.Reason = false, "non_text_output_unbounded"
					}
				}
			}
		}
	}
	if budget.InputKnown {
		encoded, err := common.Marshal(input)
		if err != nil {
			return nil, err
		}
		// Counting the wire input also includes tool history and JSON schemas that
		// the older DTO token metadata omits. CountToken never disables admission.
		budget.InputTokens = tokenkit.Count(info.UpstreamModelName, string(encoded))
	}
	return budget, nil
}

func outboundBudgetQuantity(payload map[string]json.RawMessage, fields []string, fallback int, maximum bool) (int, error) {
	value := fallback
	for _, field := range fields {
		raw, ok := payload[field]
		if !ok || string(raw) == "null" {
			continue
		}
		var quantity uint64
		if err := common.Unmarshal(raw, &quantity); err != nil || quantity > maxTokensLimit || (maximum && quantity == 0) {
			return 0, fmt.Errorf("outbound %s is invalid", field)
		}
		if !maximum || int(quantity) > value {
			value = int(quantity)
		}
	}
	return value, nil
}
