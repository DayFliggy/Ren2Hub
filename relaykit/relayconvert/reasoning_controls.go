package relayconvert

import "github.com/QuantumNous/new-api/relaykit/types"

// Provider thinking budgets and output controls have no equivalent in the
// common Chat projection. Model suffixes are not a replacement for the input.
func (r *RequestRequirements) inspectReasoningControls(payload map[string]any) {
	switch r.Format {
	case types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses:
		effort, path := payload["reasoning_effort"], "reasoning_effort"
		if r.Format == types.RelayFormatOpenAIResponses {
			config, _ := payload["reasoning"].(map[string]any)
			effort, path = config["effort"], "reasoning.effort"
			for _, field := range []string{"summary", "mode", "context"} {
				if valuePresent(config[field]) {
					r.projectionLoss("reasoning_controls_not_portable", "reasoning."+field, "conversion only preserves Responses reasoning effort", "")
				}
			}
		} else {
			for _, field := range []string{"reasoning", "thinking", "think"} {
				if valuePresent(payload[field]) {
					r.projectionLoss("reasoning_controls_not_portable", field, "conversion cannot preserve all provider reasoning controls", "")
				}
			}
		}
		for _, target := range []types.RelayFormat{types.RelayFormatClaude, types.RelayFormatGemini} {
			if valuePresent(effort) {
				r.projectionLoss("reasoning_controls_not_portable", path, "conversion cannot preserve reasoning effort without provider-specific approximations", target)
			}
			for _, field := range []string{"enable_thinking", "thinking_budget"} {
				if valuePresent(payload[field]) {
					r.Features = appendUnique(r.Features, "reasoning")
					r.projectionLoss("reasoning_controls_not_portable", field, "conversion does not preserve provider thinking controls", target)
				}
			}
		}
	case types.RelayFormatClaude:
		if valuePresent(payload["thinking"]) {
			r.Features = appendUnique(r.Features, "reasoning")
			r.projectionLoss("reasoning_controls_not_portable", "thinking", "conversion does not preserve Claude thinking controls", "")
		}
		config, _ := payload["output_config"].(map[string]any)
		if valuePresent(config["effort"]) {
			r.Features = appendUnique(r.Features, "reasoning")
			r.projectionLoss("reasoning_controls_not_portable", "output_config.effort", "conversion does not preserve Claude reasoning effort", "")
		}
	case types.RelayFormatGemini:
		config, _ := payload["generationConfig"].(map[string]any)
		if valuePresent(config["thinkingConfig"]) {
			r.Features = appendUnique(r.Features, "reasoning")
			r.projectionLoss("reasoning_controls_not_portable", "generationConfig.thinkingConfig", "conversion does not preserve Gemini thinking controls", "")
		}
	}
}
