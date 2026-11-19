package service

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// Only recognize payload-bearing fields of supported wire formats. Unknown
// events yield no latency sample rather than treating metadata as model output.
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
		"response.audio.delta", "response.output_audio.delta", "response.audio_transcript.delta":
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
		return objectHasPayload(event["content_block"], "text", "thinking", "name", "data")
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
			ToolCalls        []struct {
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
			if choice.Text != "" || d.Content != "" || d.Reasoning != "" || d.ReasoningContent != "" || d.FunctionCall.Name != "" || d.FunctionCall.Arguments != "" {
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
				}
				if common.Unmarshal(part, &media) == nil && (objectHasPayload(media.Call, "name") || objectHasPayload(media.Inline, "data")) {
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

func outputItemHasPayload(raw json.RawMessage) bool {
	if objectHasPayload(raw, "arguments", "result", "name") {
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

// requestEndReasons are end reasons the request caused: content filtering,
// safety, refusal and length limits. Matching is case-insensitive because
// Gemini reports them in upper case.
var requestEndReasons = map[string]bool{
	"content_filter": true, "content_policy_violation": true, "safety": true, "refusal": true,
	"length": true, "max_tokens": true, "max_output_tokens": true, "model_context_window_exceeded": true,
	"recitation": true, "blocklist": true, "prohibited_content": true, "spii": true, "image_safety": true,
}

// streamEventEndsForRequest reads the end reason of the supported wire
// formats: chat finish_reason, Claude stop_reason, Gemini finishReason and
// prompt block, and the Responses incomplete reason.
func streamEventEndsForRequest(data string) bool {
	var event struct {
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
			IncompleteDetails struct {
				Reason string `json:"reason"`
			} `json:"incomplete_details"`
		} `json:"response"`
	}
	if common.Unmarshal([]byte(data), &event) != nil {
		return false
	}
	reasons := []string{event.Delta.StopReason, event.Response.IncompleteDetails.Reason}
	for _, choice := range event.Choices {
		reasons = append(reasons, choice.FinishReason)
	}
	for _, candidate := range event.Candidates {
		reasons = append(reasons, candidate.FinishReason)
	}
	if event.PromptFeedback.BlockReason != "" {
		return true
	}
	for _, reason := range reasons {
		if requestEndReasons[strings.ToLower(reason)] {
			return true
		}
	}
	return false
}
