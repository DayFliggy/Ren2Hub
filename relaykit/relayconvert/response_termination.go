package relayconvert

import (
	"errors"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

var ErrResponseTerminalMissing = errors.New("response_terminal_missing: upstream response lacks a generation terminal")

// TerminationFromResponse extracts only generation semantics. An absent terminal
// is not an EOF success and must not be used to finalize an interrupted stream.
func TerminationFromResponse(response any) types.ResponseTermination {
	switch r := response.(type) {
	case dto.OpenAITextResponse:
		return TerminationFromResponse(&r)
	case dto.ChatCompletionsStreamResponse:
		return TerminationFromResponse(&r)
	case dto.OpenAIResponsesResponse:
		return TerminationFromResponse(&r)
	case dto.ResponsesStreamResponse:
		return TerminationFromResponse(&r)
	case dto.ClaudeResponse:
		return TerminationFromResponse(&r)
	case dto.GeminiChatResponse:
		return TerminationFromResponse(&r)
	case *dto.OpenAITextResponse:
		if r == nil {
			break
		}
		terminals := make([]types.ResponseTermination, 0, len(r.Choices))
		for _, choice := range r.Choices {
			terminals = append(terminals, types.ClassifyResponseTermination(types.RelayFormatOpenAI, "", choice.FinishReason))
		}
		return aggregateResponseTerminations(terminals)
	case *dto.ChatCompletionsStreamResponse:
		if r == nil {
			break
		}
		terminals := make([]types.ResponseTermination, 0, len(r.Choices))
		for _, choice := range r.Choices {
			reason := ""
			if choice.FinishReason != nil {
				reason = *choice.FinishReason
			}
			terminals = append(terminals, types.ClassifyResponseTermination(types.RelayFormatOpenAI, "", reason))
		}
		return aggregateResponseTerminations(terminals)
	case *dto.OpenAIResponsesResponse:
		if r == nil {
			break
		}
		var status string
		_ = kitutil.Unmarshal(r.Status, &status)
		reason := ""
		if r.IncompleteDetails != nil {
			reason = r.IncompleteDetails.Reason
		}
		return types.ClassifyResponseTermination(types.RelayFormatOpenAIResponses, status, reason)
	case *dto.ResponsesStreamResponse:
		if r == nil {
			break
		}
		switch r.Type {
		case "response.completed", "response.done", "response.incomplete", "response.failed", "response.cancelled", "response.canceled", "response.error", "error":
			switch r.Type {
			case "response.failed", "response.error", "error":
				return types.ResponseTermination{Kind: types.ResponseFailed}
			case "response.cancelled", "response.canceled":
				return types.ResponseTermination{Kind: types.ResponseCancelled}
			}
			status, reason := "", ""
			if r.Response != nil {
				_ = kitutil.Unmarshal(r.Response.Status, &status)
				if r.Response.IncompleteDetails != nil {
					reason = r.Response.IncompleteDetails.Reason
				}
			}
			if status == "" {
				switch r.Type {
				case "response.completed", "response.done":
					status = "completed"
				case "response.incomplete":
					status = "incomplete"
				case "response.cancelled", "response.canceled":
					status = "cancelled"
				default:
					status = "failed"
				}
			}
			if status == "in_progress" || status == "queued" {
				return types.ResponseTermination{Kind: types.ResponseUnknown}
			}
			return types.ClassifyResponseTermination(types.RelayFormatOpenAIResponses, status, reason)
		}
	case *dto.ClaudeResponse:
		if r == nil {
			break
		}
		if r.Type == "error" {
			return types.ResponseTermination{Kind: types.ResponseFailed}
		}
		reason := r.StopReason
		if r.Delta != nil && r.Delta.StopReason != nil {
			reason = *r.Delta.StopReason
		}
		return types.ClassifyResponseTermination(types.RelayFormatClaude, "", reason)
	case *dto.GeminiChatResponse:
		if r == nil {
			break
		}
		if len(r.Candidates) == 0 && r.PromptFeedback != nil && r.PromptFeedback.BlockReason != nil {
			return types.ClassifyResponseTermination(types.RelayFormatGemini, "", *r.PromptFeedback.BlockReason)
		}
		terminals := make([]types.ResponseTermination, 0, len(r.Candidates))
		for _, candidate := range r.Candidates {
			reason := ""
			if candidate.FinishReason != nil {
				reason = *candidate.FinishReason
			}
			terminals = append(terminals, types.ClassifyResponseTermination(types.RelayFormatGemini, "", reason))
		}
		return aggregateResponseTerminations(terminals)
	}
	return types.ResponseTermination{}
}

func aggregateResponseTerminations(terminals []types.ResponseTermination) types.ResponseTermination {
	var result types.ResponseTermination
	pending := false
	for _, terminal := range terminals {
		if !terminal.IsTerminal() {
			pending = true
			continue
		}
		if !terminal.IsSuccessful() {
			return terminal
		}
		if result.Kind != types.ResponseLimited {
			result = terminal
		}
	}
	if pending {
		return types.ResponseTermination{}
	}
	return result
}

func (s *ResponseStreamState) observeResponseTermination(response any) {
	terminal := TerminationFromResponse(response)
	if terminal.IsTerminal() && !terminal.IsSuccessful() {
		s.termination = terminal
		return
	}
	if s.choiceTerminations == nil {
		s.choiceTerminations = make(map[int]types.ResponseTermination)
	}
	remember := func(index int, terminal types.ResponseTermination) {
		if _, exists := s.choiceTerminations[index]; !exists || terminal.IsTerminal() {
			s.choiceTerminations[index] = terminal
		}
	}
	switch r := response.(type) {
	case dto.ChatCompletionsStreamResponse:
		s.observeResponseTermination(&r)
		return
	case dto.GeminiChatResponse:
		s.observeResponseTermination(&r)
		return
	case *dto.ChatCompletionsStreamResponse:
		for _, choice := range r.Choices {
			reason := ""
			if choice.FinishReason != nil {
				reason = *choice.FinishReason
			}
			remember(choice.Index, types.ClassifyResponseTermination(types.RelayFormatOpenAI, "", reason))
		}
	case *dto.GeminiChatResponse:
		for _, candidate := range r.Candidates {
			reason := ""
			if candidate.FinishReason != nil {
				reason = *candidate.FinishReason
			}
			remember(int(candidate.Index), types.ClassifyResponseTermination(types.RelayFormatGemini, "", reason))
		}
	default:
		if terminal.IsTerminal() {
			s.termination = terminal
		}
		return
	}
	terminals := make([]types.ResponseTermination, 0, len(s.choiceTerminations))
	for _, terminal := range s.choiceTerminations {
		terminals = append(terminals, terminal)
	}
	s.termination = aggregateResponseTerminations(terminals)
}

// ValidateResponseTermination rejects terminal states that the target cannot
// represent. This is a hard semantic constraint even under the allow policy.
func ValidateResponseTermination(from, to types.RelayFormat, terminal types.ResponseTermination) error {
	if from == to || !terminal.IsTerminal() || terminal.IsSuccessful() {
		return nil
	}
	code := "response_termination_unsupported"
	if terminal.Kind == types.ResponseProviderPause {
		code = "native_state_required"
	}
	return &types.ConversionLossError{Diagnostics: []types.ConversionDiagnostic{{
		Code: code, Path: "response.termination", Severity: types.ConversionDiagnosticError,
		From: from, To: to, Message: "target protocol cannot preserve upstream response termination",
	}}}
}
