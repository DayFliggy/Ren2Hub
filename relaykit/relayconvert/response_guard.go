package relayconvert

import (
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// guardResponseConversion rejects provider-hosted tools, citations, and
// encrypted hosted-tool state that the target protocol cannot represent.
// Reasoning signatures (Claude thinking, Gemini thought signatures) are not
// portable either, but the response itself is: converters never copy them, so
// their presence is reported once as provider_state_dropped, which the safe
// and allow policies pass and strict rejects. Tool media hoisting is
// independent from this boundary.
func guardResponseConversion(from, to types.RelayFormat, response any, info convmeta.Meta, diagnostics []types.ConversionDiagnostic) ([]types.ConversionDiagnostic, error) {
	if from == to {
		return diagnostics, nil
	}
	raw, err := kitutil.Marshal(response)
	if err != nil {
		return nil, err
	}
	var body any
	if err = kitutil.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	stateDropped, err := guardResponseState(body, to)
	if err != nil {
		return nil, err
	}
	if stateDropped {
		diagnostics = append(diagnostics, types.ConversionDiagnostic{
			Code:     "provider_state_dropped",
			Message:  "provider reasoning signatures cannot cross protocols and were dropped",
			Severity: types.ConversionDiagnosticWarning,
			From:     from,
			To:       to,
		})
	}
	return diagnostics, types.RejectConversionLoss(convmeta.OptionsOf(info).EffectiveConversionPolicy(), diagnostics)
}

// guardResponseState reports whether the response carries reasoning
// signatures and fails on hosted tools, citations, or encrypted hosted-tool
// state the target cannot represent.
func guardResponseState(value any, target types.RelayFormat) (bool, error) {
	stateDropped := false
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			dropped, err := guardResponseState(item, target)
			if err != nil {
				return false, err
			}
			stateDropped = stateDropped || dropped
		}
	case map[string]any:
		kind, _ := value["type"].(string)
		if kind == "server_tool_use" && value["name"] != "web_search" && value["name"] != "web_fetch" {
			return false, fmt.Errorf("hosted_tool_unrepresentable: unsupported provider hosted tool")
		}
		if target == types.RelayFormatClaude && kind == "web_search_call" {
			return false, fmt.Errorf("native_state_required: web search cannot recreate Claude continuation state")
		}
		if kind == "file_citation" || kind == "container_file_citation" {
			return false, fmt.Errorf("citation_source_unrepresentable: provider file citation requires native protocol")
		}
		if target == types.RelayFormatClaude && kind == "url_citation" {
			return false, fmt.Errorf("native_state_required: Claude citation requires provider-issued source state")
		}
		if kind == "redacted_thinking" && value["data"] != nil && value["data"] != "" {
			// Claude's encrypted thinking block is a reasoning signature with no
			// visible text; converters never copy its data.
			stateDropped = true
		}
		for key, item := range value {
			if item != nil && item != "" {
				switch key {
				case "signature", "thoughtSignature":
					stateDropped = true
				case "encrypted_content", "encrypted_index":
					// Encrypted hosted-tool results and citation locations are
					// part of the visible answer, so they are not dropped.
					return false, fmt.Errorf("native_state_required: provider hosted-tool or citation state cannot cross protocols")
				}
			}
			// Traverse protocol envelopes only. Tool arguments and application
			// metadata may legitimately contain keys such as "signature".
			switch key {
			case "response", "output", "content", "content_block", "delta", "choices", "message", "candidates", "parts", "annotations", "citations", "item":
				dropped, err := guardResponseState(item, target)
				if err != nil {
					return false, err
				}
				stateDropped = stateDropped || dropped
			}
		}
	}
	return stateDropped, nil
}
