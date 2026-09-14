package relaycapability

import (
	"fmt"
	"slices"

	"github.com/QuantumNous/new-api/relaykit/relayconvert"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// TextProjection describes a host text converter with explicitly enabled images.
// Fields are the incoming Chat fields consumed by that concrete converter.
type TextProjection struct {
	ID                  string
	Fields              []string
	MessageFields       map[string][]string
	Roles               []string
	LastRole            string
	SystemAtStart       bool
	Images              bool
	SingleTurn          bool
	ConvertedInputs     bool
	StrictRoles         bool
	UnsupportedFeatures map[string]string
}

func (p *TextProjection) Check(req relayconvert.RequestRequirements, policy types.ConversionLossPolicy) ([]types.ConversionDiagnostic, error) {
	if p == nil {
		return nil, nil
	}
	var diagnostics []types.ConversionDiagnostic
	add := func(code, path, message string, severity types.ConversionDiagnosticSeverity) {
		diagnostics = append(diagnostics, types.ConversionDiagnostic{
			Code: code, Path: path, Message: message, Severity: severity, From: req.Format, To: types.RelayFormatOpenAI,
		})
	}
	for _, feature := range req.Features {
		if message, unsupported := p.UnsupportedFeatures[feature]; unsupported {
			add("projection_feature_unavailable", feature, message, types.ConversionDiagnosticError)
		}
	}
	if len(diagnostics) != 0 {
		return diagnostics, &types.ConversionLossError{Diagnostics: diagnostics}
	}
	if req.Format != types.RelayFormatOpenAI {
		if p.ConvertedInputs {
			// The protocol plan validates the source conversion. These host
			// feature constraints also apply before that conversion runs.
			return nil, nil
		}
		return nil, fmt.Errorf("text projection requires analyzed Chat request structure")
	}
	if req.Shape == nil {
		return nil, fmt.Errorf("text projection requires analyzed Chat request structure")
	}
	if req.NativeRequired {
		add("native_state_required", "messages", "provider-owned state cannot use a text projection", types.ConversionDiagnosticError)
	}
	if p.SingleTurn && len(req.Shape.Messages) > 1 {
		add("projection_history_flattened", "messages", "conversation history becomes a single prompt for "+p.ID, types.ConversionDiagnosticError)
	}
	for _, field := range req.Shape.Fields {
		if slices.Contains(p.Fields, field) {
			continue
		}
		severity := types.ConversionDiagnosticError
		switch field {
		case "metadata", "user", "temperature", "top_p", "top_k", "frequency_penalty", "presence_penalty", "seed", "logprobs", "top_logprobs":
			severity = types.ConversionDiagnosticWarning
		}
		add("projection_field_dropped", field, "request field is not implemented by "+p.ID, severity)
	}
	invalidOrder := false
	if p.LastRole != "" && len(req.Shape.Messages) > 0 && req.Shape.Messages[len(req.Shape.Messages)-1].Role != p.LastRole {
		add("projection_message_order", "messages", "the final message must have role "+p.LastRole+" for "+p.ID, types.ConversionDiagnosticError)
		invalidOrder = true
	}
	seenConversation := false
	for index, message := range req.Shape.Messages {
		path := fmt.Sprintf("messages[%d]", index)
		if message.Role == "system" {
			if p.SystemAtStart && seenConversation {
				add("projection_message_order", path+".role", "system messages must precede the conversation for "+p.ID, types.ConversionDiagnosticError)
				invalidOrder = true
			}
		} else {
			seenConversation = true
		}
		if !slices.Contains(p.Roles, message.Role) {
			add("projection_role_dropped", path+".role", "message role is not implemented by "+p.ID, types.ConversionDiagnosticError)
			invalidOrder = invalidOrder || p.StrictRoles
		}
		for _, field := range message.Fields {
			if field == "role" || field == "content" || slices.Contains(p.MessageFields[message.Role], field) {
				continue
			}
			severity := types.ConversionDiagnosticError
			if field == "name" {
				severity = types.ConversionDiagnosticWarning
			}
			add("projection_message_field_dropped", path+"."+field, "message field is not implemented by "+p.ID, severity)
		}
		for partIndex, part := range message.Content {
			partPath := fmt.Sprintf("%s.content[%d]", path, partIndex)
			if part.Type != "text" && !(p.Images && part.Type == "image_url") {
				add("projection_content_dropped", partPath, "only text content is implemented by "+p.ID, types.ConversionDiagnosticError)
				continue
			}
			for _, field := range part.Fields {
				if field != "type" && field != "text" && !(p.Images && (field == "image_url" || field == "image_url.url")) {
					add("projection_content_field_dropped", partPath+"."+field, "text content field is not implemented by "+p.ID, types.ConversionDiagnosticWarning)
				}
			}
		}
	}
	if invalidOrder {
		return diagnostics, &types.ConversionLossError{Diagnostics: diagnostics}
	}
	return diagnostics, types.RejectConversionLoss(policy, diagnostics)
}
