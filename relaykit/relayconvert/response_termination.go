package relayconvert

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

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
		if r != nil && len(r.Choices) > 0 {
			return types.ClassifyResponseTermination(types.RelayFormatOpenAI, "", r.Choices[0].FinishReason)
		}
	case *dto.ChatCompletionsStreamResponse:
		if r != nil && len(r.Choices) > 0 && r.Choices[0].FinishReason != nil {
			return types.ClassifyResponseTermination(types.RelayFormatOpenAI, "", *r.Choices[0].FinishReason)
		}
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
		if r != nil && len(r.Candidates) > 0 && r.Candidates[0].FinishReason != nil {
			return types.ClassifyResponseTermination(types.RelayFormatGemini, "", *r.Candidates[0].FinishReason)
		}
	}
	return types.ResponseTermination{}
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
