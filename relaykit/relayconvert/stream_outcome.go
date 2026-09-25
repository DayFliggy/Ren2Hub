package relayconvert

import (
	"context"
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	oaichat "github.com/QuantumNous/new-api/relaykit/relayconvert/internal/oai_chat"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type StreamOutcome string

var ErrStreamTerminalMissing = errors.New("stream_terminal_missing: all upstream candidates must have an explicit terminal")

const (
	StreamOutcomeCompleted  StreamOutcome = "completed"
	StreamOutcomeIncomplete StreamOutcome = "incomplete"
	StreamOutcomeFailed     StreamOutcome = "failed"
	StreamOutcomeCancelled  StreamOutcome = "cancelled"
)

// FinalizeStreamResponseWithOutcome prevents an abnormal transport end from
// manufacturing a successful protocol terminal event. Completed means the host
// observed a valid upstream protocol terminal, not merely an HTTP EOF.
func FinalizeStreamResponseWithOutcome(ctx context.Context, info convmeta.Meta, state *ResponseStreamState, outcome StreamOutcome) ([]ResponseResult, error) {
	if state == nil {
		return nil, fmt.Errorf("response stream state is required")
	}
	if state.finalized {
		return nil, nil
	}
	switch outcome {
	case StreamOutcomeCompleted:
		if state.conversionError != nil {
			return nil, state.conversionError
		}
		if state.From != state.To && !state.Termination().IsTerminal() {
			return nil, ErrStreamTerminalMissing
		}
		if err := ValidateResponseTermination(state.From, state.To, state.Termination()); err != nil {
			return nil, err
		}
		return finalizeStreamResponse(ctx, info, state)
	case StreamOutcomeIncomplete, StreamOutcomeFailed, StreamOutcomeCancelled:
	default:
		return nil, fmt.Errorf("invalid stream outcome %q", outcome)
	}
	state.finalized = true
	if outcome == StreamOutcomeCancelled {
		return nil, nil
	}
	if state.To == types.RelayFormatOpenAIResponses {
		for _, value := range state.stepStates {
			if converted, ok := value.(*oaichat.ChatToResponsesStreamState); ok {
				var events []oaichat.ChatToResponsesStreamEvent
				if outcome == StreamOutcomeIncomplete {
					events = converted.EndIncomplete()
				} else {
					events = converted.Fail("upstream_stream_interrupted", "upstream stream ended before terminal", "")
				}
				values := make([]any, len(events))
				for i := range events {
					values[i] = events[i]
				}
				return responseStreamResults(state, values, state.Usage(), nil), nil
			}
		}
	}
	message := "stream_interrupted: upstream stream ended without a successful terminal event"
	var value any
	switch state.To {
	case types.RelayFormatClaude:
		value = &dto.ClaudeResponse{Type: "error", Error: &types.ClaudeError{Type: "api_error", Message: message}}
	case types.RelayFormatOpenAIResponses:
		value = &dto.ResponsesStreamResponse{Type: "error", Code: "upstream_stream_error", Message: message}
	default:
		return nil, fmt.Errorf("%s: %s", outcome, message)
	}
	return responseStreamResults(state, []any{value}, state.Usage(), nil), nil
}
