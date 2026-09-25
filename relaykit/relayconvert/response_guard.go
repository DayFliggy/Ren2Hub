package relayconvert

import (
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// A provider-issued continuation proof cannot be recreated by another protocol.
// Tool media hoisting is independent from this native-state boundary.
func guardResponseConversion(from, to types.RelayFormat, response any, info convmeta.Meta, diagnostics []types.ConversionDiagnostic) error {
	if from == to {
		return nil
	}
	raw, err := kitutil.Marshal(response)
	if err != nil {
		return err
	}
	var body any
	if err = kitutil.Unmarshal(raw, &body); err != nil {
		return err
	}
	if err = guardResponseState(body, to); err != nil {
		return err
	}
	return types.RejectConversionLoss(convmeta.OptionsOf(info).EffectiveConversionPolicy(), diagnostics)
}
func guardResponseState(value any, target types.RelayFormat) error {
	switch value := value.(type) {
	case []any:
		for _, item := range value {
			if err := guardResponseState(item, target); err != nil {
				return err
			}
		}
	case map[string]any:
		kind, _ := value["type"].(string)
		if kind == "server_tool_use" && value["name"] != "web_search" && value["name"] != "web_fetch" {
			return fmt.Errorf("hosted_tool_unrepresentable: unsupported provider hosted tool")
		}
		if target == types.RelayFormatClaude && kind == "web_search_call" {
			return fmt.Errorf("native_state_required: web search cannot recreate Claude continuation state")
		}
		if kind == "file_citation" || kind == "container_file_citation" {
			return fmt.Errorf("citation_source_unrepresentable: provider file citation requires native protocol")
		}
		if target == types.RelayFormatClaude && kind == "url_citation" {
			return fmt.Errorf("native_state_required: Claude citation requires provider-issued source state")
		}
		for key, item := range value {
			switch key {
			case "encrypted_content", "encrypted_index", "thoughtSignature", "signature":
				if item != nil && item != "" {
					return fmt.Errorf("native_state_required: provider continuation state cannot cross protocols")
				}
			}
			// Traverse protocol envelopes only. Tool arguments and application
			// metadata may legitimately contain keys such as "signature".
			switch key {
			case "response", "output", "content", "content_block", "delta", "choices", "message", "candidates", "parts", "annotations", "citations", "item":
				if err := guardResponseState(item, target); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
