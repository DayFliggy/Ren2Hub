package toolconv

import (
	"bytes"
	"fmt"

	"github.com/QuantumNous/new-api/relaykit/dto"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
)

type legacyFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// legacyFunctionPairs maps the index of an assistant message carrying a legacy
// function_call to its call. Only calls answered by the immediately following
// role=function message with the same name are paired: that is the legacy
// protocol, and every target requires the result right after its call.
type legacyFunctionPairs map[int]legacyFunctionCall

func planLegacyFunctionPairs(messages []dto.Message) (legacyFunctionPairs, []types.ConversionDiagnostic, error) {
	pairs := legacyFunctionPairs{}
	answered := map[int]bool{}
	var diagnostics []types.ConversionDiagnostic
	for index, message := range messages {
		if message.Role == "function" && message.ToolCallId == "" && !answered[index] {
			diagnostics = append(diagnostics, types.ConversionDiagnostic{
				Code: "legacy_function_result_unpaired", Path: fmt.Sprintf("messages[%d]", index), Severity: types.ConversionDiagnosticError,
				Message: "role=function result does not follow a legacy function_call with the same name; conversion cannot express it as a tool result",
			})
		}
		if message.Role != "assistant" || legacyFunctionCallAbsent(message.FunctionCall) {
			continue
		}
		var call legacyFunctionCall
		if err := kitutil.Unmarshal(message.FunctionCall, &call); err != nil {
			return nil, nil, fmt.Errorf("messages[%d].function_call is invalid: %w", index, err)
		}
		if call.Name == "" {
			return nil, nil, fmt.Errorf("messages[%d].function_call requires name", index)
		}
		next := index + 1
		if next < len(messages) && messages[next].Role == "function" && messages[next].ToolCallId == "" &&
			messages[next].Name != nil && *messages[next].Name == call.Name {
			pairs[index] = call
			answered[next] = true
			continue
		}
		diagnostics = append(diagnostics, types.ConversionDiagnostic{
			Code: "legacy_function_call_unpaired", Path: fmt.Sprintf("messages[%d].function_call", index), Severity: types.ConversionDiagnosticError,
			Message: "legacy function_call has no immediately following role=function result with the same name; conversion omits the call",
		})
	}
	return pairs, diagnostics, nil
}

func legacyFunctionCallAbsent(raw []byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null"))
}

// LegacyFunctionHistoryDiagnostics reports legacy calls and results that
// cannot be paired. The diagnostics do not depend on the target protocol.
func LegacyFunctionHistoryDiagnostics(messages []dto.Message) ([]types.ConversionDiagnostic, error) {
	_, diagnostics, err := planLegacyFunctionPairs(messages)
	return diagnostics, err
}

// PairLegacyFunctionHistory returns a copy of messages in which every paired
// legacy call becomes an assistant tool call and its result a tool message.
// Unpaired calls are dropped and unpaired results keep their legacy role; the
// planner reports both through LegacyFunctionHistoryDiagnostics. Gemini matches
// results by name, so its calls get no identifier; other targets get a stable
// synthetic identifier derived from message order, so replaying the same
// history yields the same request. The input is never modified.
func PairLegacyFunctionHistory(messages []dto.Message, target types.RelayFormat) ([]dto.Message, error) {
	pairs, _, err := planLegacyFunctionPairs(messages)
	if err != nil {
		return nil, err
	}
	ids := newLegacyCallIDs(messages, target != types.RelayFormatGemini)
	out := make([]dto.Message, len(messages))
	copy(out, messages)
	for index := range out {
		message := &out[index]
		if message.Role != "assistant" || legacyFunctionCallAbsent(message.FunctionCall) {
			continue
		}
		message.FunctionCall = nil
		call, paired := pairs[index]
		if !paired {
			continue
		}
		id := ids.next()
		toolCalls := append(message.ParseToolCalls(), dto.ToolCallRequest{
			ID: id, Type: "function", Function: dto.FunctionRequest{Name: call.Name, Arguments: call.Arguments},
		})
		encoded, err := kitutil.Marshal(toolCalls)
		if err != nil {
			return nil, fmt.Errorf("messages[%d].function_call: %w", index, err)
		}
		message.ToolCalls = encoded
		out[index+1].Role, out[index+1].ToolCallId = "tool", id
	}
	return out, nil
}

type legacyCallIDs struct {
	messages []dto.Message
	enabled  bool
	used     map[string]bool
	count    int
}

func newLegacyCallIDs(messages []dto.Message, enabled bool) *legacyCallIDs {
	return &legacyCallIDs{messages: messages, enabled: enabled}
}

func (ids *legacyCallIDs) next() string {
	if !ids.enabled {
		return ""
	}
	if ids.used == nil {
		// Client identifiers are collected only once a pair needs an id.
		ids.used = map[string]bool{}
		for _, message := range ids.messages {
			ids.used[message.ToolCallId] = true
			for _, call := range message.ParseToolCalls() {
				ids.used[call.ID] = true
			}
		}
	}
	for {
		ids.count++
		id := fmt.Sprintf("call_legacy_%d", ids.count)
		if !ids.used[id] {
			ids.used[id] = true
			return id
		}
	}
}
