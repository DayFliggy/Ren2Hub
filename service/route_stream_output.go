package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Only recognize payload-bearing fields of supported wire formats, as defined
// by the relaykit DTOs and the pass-through wire formats. Unknown events yield
// no latency sample rather than treating metadata as model output. The same
// check runs on raw upstream frames (ObserveRouteUsage): a stream without
// recognized client content counts as empty only when the raw frames show a
// normal completion (streamEventEnd) and no payload. A raw payload this check
// does not recognize, followed by a normal completion, therefore still counts
// as empty; only a Responses terminal is stricter, where listed items that are
// not known empty messages (responsesOutputEmpty) count as payload.
func streamEventHasOutput(data string) bool {
	var event map[string]json.RawMessage
	if common.Unmarshal([]byte(data), &event) != nil || (len(event["error"]) > 0 && !bytes.Equal(event["error"], []byte("null"))) {
		return false
	}
	var kind string
	_ = common.Unmarshal(event["type"], &kind)
	switch kind {
	case "response.output_text.delta", "response.text.delta", "response.refusal.delta", "response.reasoning_text.delta",
		"response.reasoning_summary_text.delta", "response.function_call_arguments.delta",
		"response.audio.delta", "response.output_audio.delta", "response.audio_transcript.delta", "response.output_audio_transcript.delta",
		"response.custom_tool_call_input.delta", "response.mcp_call_arguments.delta":
		return nonemptyString(event["delta"])
	case "image_generation.partial_image", "image_generation.completed", "image_edit.partial_image", "image_edit.completed":
		return nonemptyString(event["b64_json"]) || nonemptyString(event["url"])
	case "response.image_generation_call.partial_image":
		return nonemptyString(event["partial_image_b64"])
	case "speech.audio.delta":
		return nonemptyString(event["audio"])
	case "transcript.text.delta":
		return nonemptyString(event["delta"])
	case "content_block_delta":
		return objectHasPayload(event["delta"], "text", "thinking", "partial_json", "data")
	case "content_block_start":
		// Tool-result blocks of server tools carry their result in content.
		return objectHasPayload(event["content_block"], "text", "thinking", "name", "data") || objectHasValue(event["content_block"], "content")
	case "completion":
		return nonemptyString(event["completion"])
	case "response.output_item.added", "response.output_item.done":
		return outputItemHasPayload(event["item"])
	case "response.completed", "response.done", "response.incomplete":
		var response struct {
			Output []json.RawMessage `json:"output"`
		}
		if common.Unmarshal(event["response"], &response) == nil {
			for _, item := range response.Output {
				if outputItemHasPayload(item) {
					return true
				}
			}
		}
	}
	var choices []struct {
		Text  string `json:"text"`
		Delta struct {
			Content          string `json:"content"`
			Reasoning        string `json:"reasoning"`
			ReasoningContent string `json:"reasoning_content"`
			Refusal          string `json:"refusal"`
			// Only data and transcript are output; id and expires_at are metadata.
			Audio struct {
				Data       string `json:"data"`
				Transcript string `json:"transcript"`
			} `json:"audio"`
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
			FunctionCall struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function_call"`
		} `json:"delta"`
	}
	if common.Unmarshal(event["choices"], &choices) == nil {
		for _, choice := range choices {
			d := choice.Delta
			if choice.Text != "" || d.Content != "" || d.Reasoning != "" || d.ReasoningContent != "" || d.Refusal != "" ||
				d.Audio.Data != "" || d.Audio.Transcript != "" || d.FunctionCall.Name != "" || d.FunctionCall.Arguments != "" {
				return true
			}
			for _, tool := range d.ToolCalls {
				if tool.Function.Name != "" || tool.Function.Arguments != "" {
					return true
				}
			}
		}
	}
	var candidates []struct {
		Content struct {
			Parts []json.RawMessage `json:"parts"`
		} `json:"content"`
	}
	if common.Unmarshal(event["candidates"], &candidates) == nil {
		for _, candidate := range candidates {
			for _, part := range candidate.Content.Parts {
				if objectHasPayload(part, "text") {
					return true
				}
				var media struct {
					Call   json.RawMessage `json:"functionCall"`
					Inline json.RawMessage `json:"inlineData"`
					File   json.RawMessage `json:"fileData"`
					Code   json.RawMessage `json:"executableCode"`
					Result json.RawMessage `json:"codeExecutionResult"`
				}
				if common.Unmarshal(part, &media) == nil && (objectHasPayload(media.Call, "name") || objectHasPayload(media.Inline, "data") ||
					objectHasPayload(media.File, "fileUri") || objectHasPayload(media.Code, "code") || objectHasPayload(media.Result, "output", "outcome")) {
					return true
				}
			}
		}
	}
	return false
}

func nonemptyString(raw json.RawMessage) bool {
	var value string
	return common.Unmarshal(raw, &value) == nil && value != ""
}

func objectHasPayload(raw json.RawMessage, fields ...string) bool {
	var object map[string]json.RawMessage
	if common.Unmarshal(raw, &object) != nil {
		return false
	}
	for _, field := range fields {
		if nonemptyString(object[field]) {
			return true
		}
	}
	return false
}

// objectHasValue reports whether any of the fields holds a non-empty JSON
// value: not null, an empty string, an empty array or an empty object,
// whatever the whitespace.
func objectHasValue(raw json.RawMessage, fields ...string) bool {
	var object map[string]json.RawMessage
	if common.Unmarshal(raw, &object) != nil {
		return false
	}
	for _, field := range fields {
		var value any
		if len(object[field]) == 0 || common.Unmarshal(object[field], &value) != nil {
			continue
		}
		switch v := value.(type) {
		case nil:
		case string:
			if v != "" {
				return true
			}
		case []any:
			if len(v) > 0 {
				return true
			}
		case map[string]any:
			if len(v) > 0 {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// outputItemHasPayload covers the Responses output items: messages, reasoning,
// function and custom tool calls, and hosted tools such as web search, file
// search, computer use, code interpreter, image generation, shell and MCP,
// which may carry only an action, queries, code or results.
func outputItemHasPayload(raw json.RawMessage) bool {
	if objectHasPayload(raw, "arguments", "result", "name") ||
		objectHasValue(raw, "input", "action", "queries", "results", "code", "outputs", "output", "tools") {
		return true
	}
	var item struct {
		Content []json.RawMessage `json:"content"`
		Summary []json.RawMessage `json:"summary"`
	}
	if common.Unmarshal(raw, &item) != nil {
		return false
	}
	for _, part := range append(item.Content, item.Summary...) {
		if objectHasPayload(part, "text", "refusal") {
			return true
		}
	}
	return false
}

// streamEnd is what one stream event says about the end of the generation.
type streamEnd int

const (
	streamEndNone streamEnd = iota
	// streamEndNormal: the upstream says the generation completed normally.
	streamEndNormal
	// streamEndRequest: the request caused the end (filtering, safety,
	// refusal or a length limit).
	streamEndRequest
	// streamEndUnproven: a completed Responses terminal lists output items
	// that are neither recognized content nor known empty messages, such as
	// encrypted reasoning; they are payload, not proof of an empty response.
	streamEndUnproven
)

// requestEndReasons are end reasons the request caused. Matching is
// case-insensitive because Gemini reports reasons in upper case.
var requestEndReasons = map[string]bool{
	"content_filter": true, "content_policy_violation": true, "safety": true, "refusal": true,
	"length": true, "max_tokens": true, "max_output_tokens": true, "model_context_window_exceeded": true,
	"recitation": true, "blocklist": true, "prohibited_content": true, "spii": true, "image_safety": true,
}

// normalEndReasons are the normal completions: chat stop and tool calls,
// Claude end_turn, stop_sequence and tool_use, Gemini STOP.
var normalEndReasons = map[string]bool{
	"stop": true, "tool_calls": true, "function_call": true,
	"end_turn": true, "stop_sequence": true, "tool_use": true,
}

// streamEventEnd reads the end of the supported wire formats: chat
// finish_reason, Claude stop_reason and message_stop, Gemini finishReason and
// prompt block, and the Responses terminal and incomplete reason. A Responses
// terminal (response.completed, or response.done with status completed, as the
// native handler accepts both) is a normal completion only when its output is
// proven empty (responsesOutputEmpty); otherwise it is streamEndUnproven. A
// request-caused reason wins over a normal one in the same event.
func streamEventEnd(data string) streamEnd {
	var event struct {
		Type    string `json:"type"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Delta struct {
			StopReason string `json:"stop_reason"`
		} `json:"delta"`
		Candidates []struct {
			FinishReason string `json:"finishReason"`
		} `json:"candidates"`
		PromptFeedback struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
		Response struct {
			Status            string            `json:"status"`
			Output            []json.RawMessage `json:"output"`
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
		} `json:"response"`
	}
	if common.Unmarshal([]byte(data), &event) != nil {
		return streamEndNone
	}
	if event.PromptFeedback.BlockReason != "" {
		return streamEndRequest
	}
	reasons := []string{event.Delta.StopReason, event.Response.IncompleteDetails.Reason}
	for _, choice := range event.Choices {
		reasons = append(reasons, choice.FinishReason)
	}
	for _, candidate := range event.Candidates {
		reasons = append(reasons, candidate.FinishReason)
	}
	end := streamEndNone
	switch event.Type {
	case "message_stop":
		end = streamEndNormal
	case "response.completed", "response.done":
		if event.Type == "response.completed" || event.Response.Status == "completed" {
			if !responsesOutputEmpty(event.Response.Output) {
				return streamEndUnproven
			}
			end = streamEndNormal
		}
	}
	for _, reason := range reasons {
		reason = strings.ToLower(reason)
		if requestEndReasons[reason] {
			return streamEndRequest
		}
		if normalEndReasons[reason] {
			end = streamEndNormal
		}
	}
	return end
}

// responsesOutputEmpty reports a Responses output that is proven empty:
// missing, an empty array, or only message items whose content is empty or
// holds only text parts that carry nothing (emptyTextPart), the common shape
// of an empty answer from a reseller.
func responsesOutputEmpty(output []json.RawMessage) bool {
	for _, raw := range output {
		var item struct {
			Type    string            `json:"type"`
			Content []json.RawMessage `json:"content"`
		}
		if common.Unmarshal(raw, &item) != nil || item.Type != "message" {
			return false
		}
		for _, part := range item.Content {
			if !emptyTextPart(part) {
				return false
			}
		}
	}
	return true
}

// emptyTextPart reports a text part whose every member other than its type
// is empty: an empty text with annotations, such as a file citation, or any
// other filled member is output the client receives.
func emptyTextPart(raw json.RawMessage) bool {
	var part map[string]json.RawMessage
	var kind string
	if common.Unmarshal(raw, &part) != nil || common.Unmarshal(part["type"], &kind) != nil || kind != "output_text" && kind != "text" {
		return false
	}
	for field := range part {
		if field != "type" && objectHasValue(raw, field) {
			return false
		}
	}
	return true
}
