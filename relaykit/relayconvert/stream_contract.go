package relayconvert

import (
	"errors"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

var ErrIncompleteStream = errors.New("upstream stream ended without a terminal event")

// SawTerminalEvent is evidence from the source protocol, not a terminal event
// synthesized by a target converter's Finalize method. For multi-choice streams
// every observed choice must finish before EOF is accepted.
func (s *ResponseStreamState) SawTerminalEvent() bool {
	if s == nil {
		return false
	}
	if len(s.sourceChoices) > 0 {
		for _, done := range s.sourceChoices {
			if !done {
				return false
			}
		}
		return true
	}
	return s.sourceTerminal
}

func (s *ResponseStreamState) DiagnosticsTruncated() bool { return s != nil && s.diagnosticsTruncated }

func (s *ResponseStreamState) rememberSourceTerminal(response any) {
	switch value := response.(type) {
	case dto.ChatCompletionsStreamResponse:
		s.rememberSourceTerminal(&value)
	case dto.GeminiChatResponse:
		s.rememberSourceTerminal(&value)
	case dto.ClaudeResponse:
		s.rememberSourceTerminal(&value)
	case dto.ResponsesStreamResponse:
		s.rememberSourceTerminal(&value)
	case *dto.ChatCompletionsStreamResponse:
		for _, choice := range value.Choices {
			s.rememberChoice(int64(choice.Index), choice.FinishReason != nil && *choice.FinishReason != "")
		}
	case *dto.GeminiChatResponse:
		for _, choice := range value.Candidates {
			s.rememberChoice(choice.Index, choice.FinishReason != nil && *choice.FinishReason != "")
		}
	case *dto.ClaudeResponse:
		s.sourceTerminal = s.sourceTerminal || value.Type == "message_stop" || value.Type == "error"
	case *dto.ResponsesStreamResponse:
		switch value.Type {
		case "response.completed", "response.done", "response.incomplete", "response.failed", "error":
			s.sourceTerminal = true
		}
	}
}

func (s *ResponseStreamState) rememberChoice(index int64, done bool) {
	if s.sourceChoices == nil {
		s.sourceChoices = make(map[int64]bool)
	}
	s.sourceChoices[index] = s.sourceChoices[index] || done
}
