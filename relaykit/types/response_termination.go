package types

import "strings"

// ResponseTermination describes generation semantics, independently of whether
// the HTTP/SSE transport completed normally. The zero value is non-terminal.
type ResponseTermination struct {
	Kind   ResponseTerminationKind `json:"kind,omitempty"`
	Reason string                  `json:"reason,omitempty"`
}

type ResponseTerminationKind string

const (
	ResponseCompleted     ResponseTerminationKind = "completed"
	ResponseLimited       ResponseTerminationKind = "limited"
	ResponseProviderPause ResponseTerminationKind = "provider_pause"
	ResponseFailed        ResponseTerminationKind = "failed"
	ResponseCancelled     ResponseTerminationKind = "cancelled"
	ResponseUnknown       ResponseTerminationKind = "unknown"
)

func (t ResponseTermination) IsTerminal() bool { return t.Kind != "" }

func (t ResponseTermination) IsSuccessful() bool {
	return t.Kind == ResponseCompleted || t.Kind == ResponseLimited
}

// ClassifyResponseTermination accepts protocol status and finish/stop reason.
// Unknown terminal reasons deliberately remain unknown rather than success.
func ClassifyResponseTermination(format RelayFormat, status, reason string) ResponseTermination {
	status, reason = strings.ToLower(strings.TrimSpace(status)), strings.ToLower(strings.TrimSpace(reason))
	t := ResponseTermination{Reason: reason}
	if status == "failed" || status == "error" {
		t.Kind = ResponseFailed
		return t
	}
	if status == "cancelled" || status == "canceled" {
		t.Kind = ResponseCancelled
		return t
	}
	if reason == "pause_turn" {
		t.Kind = ResponseProviderPause
		return t
	}
	switch format {
	case RelayFormatOpenAIResponses:
		switch status {
		case "completed":
			t.Kind = ResponseCompleted
		case "incomplete":
			switch reason {
			case "max_output_tokens", "content_filter":
				t.Kind = ResponseLimited
			default:
				t.Kind = ResponseUnknown
			}
		case "", "in_progress", "queued":
		default:
			t.Kind = ResponseUnknown
		}
	case RelayFormatClaude:
		switch reason {
		case "end_turn", "stop_sequence", "tool_use":
			t.Kind = ResponseCompleted
		case "max_tokens", "refusal", "model_context_window_exceeded":
			t.Kind = ResponseLimited
		case "":
		default:
			t.Kind = ResponseUnknown
		}
	case RelayFormatGemini:
		switch reason {
		case "stop":
			t.Kind = ResponseCompleted
		case "max_tokens", "safety", "recitation", "blocklist", "prohibited_content", "spii", "image_safety", "image_prohibited_content":
			t.Kind = ResponseLimited
		case "", "finish_reason_unspecified":
		default:
			t.Kind = ResponseUnknown
		}
	default:
		switch reason {
		case "stop", "tool_calls", "function_call":
			t.Kind = ResponseCompleted
		case "length", "content_filter":
			t.Kind = ResponseLimited
		case "":
		default:
			t.Kind = ResponseUnknown
		}
	}
	return t
}
