package router

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func gatewaySDKToolResponse(w http.ResponseWriter, r *http.Request, modelName string, stream bool) {
	w.Header().Set("Content-Type", "application/json")
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
	}
	write := func(value map[string]any) {
		data, _ := common.Marshal(value)
		if !stream {
			_, _ = w.Write(data)
			return
		}
		if event, ok := value["type"]; ok {
			_, _ = fmt.Fprintf(w, "event: %s\n", event)
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	args := map[string]any{"value": "example"}
	switch {
	case r.URL.Path == "/api/chat":
		// Ollama supplies structured arguments and need not supply a call ID.
		response := map[string]any{"model": modelName, "message": map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"function": map[string]any{"name": "echo", "arguments": args}},
		}}, "done": true, "done_reason": "stop", "prompt_eval_count": 10, "eval_count": 2}
		data, _ := common.Marshal(response)
		if stream {
			w.Header().Set("Content-Type", "application/x-ndjson")
		}
		_, _ = fmt.Fprintf(w, "%s\n", data)
	case strings.Contains(r.URL.Path, "generateContent") || strings.Contains(r.URL.Path, "streamGenerateContent"):
		write(map[string]any{"candidates": []any{map[string]any{"index": 0, "content": map[string]any{"role": "model", "parts": []any{map[string]any{"functionCall": map[string]any{"id": "call_sdk", "name": "echo", "args": args}}}}, "finishReason": "STOP"}}, "usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 2, "totalTokenCount": 12}})
	case strings.Contains(r.URL.Path, "messages"):
		item := map[string]any{"type": "tool_use", "id": "call_sdk", "name": "echo", "input": args}
		usage := map[string]any{"input_tokens": 10, "output_tokens": 2}
		if !stream {
			write(map[string]any{"id": "msg_tool_sdk", "type": "message", "role": "assistant", "model": modelName, "content": []any{item}, "stop_reason": "tool_use", "stop_sequence": nil, "usage": usage})
			return
		}
		for _, event := range []map[string]any{
			{"type": "message_start", "message": map[string]any{"id": "msg_tool_sdk", "type": "message", "role": "assistant", "model": modelName, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": usage}},
			{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "tool_use", "id": "call_sdk", "name": "echo", "input": map[string]any{}}},
			{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "input_json_delta", "partial_json": `{"value":"example"}`}},
			{"type": "content_block_stop", "index": 0},
			{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use", "stop_sequence": nil}, "usage": usage}, {"type": "message_stop"},
		} {
			write(event)
		}
	case strings.Contains(r.URL.Path, "responses"):
		item := map[string]any{"id": "fc_sdk", "type": "function_call", "call_id": "call_sdk", "name": "echo", "arguments": `{"value":"example"}`, "status": "completed"}
		response := map[string]any{"id": "resp_tool_sdk", "object": "response", "created_at": 1, "status": "completed", "model": modelName, "output": []any{item}, "usage": map[string]any{"input_tokens": 10, "output_tokens": 2, "total_tokens": 12}}
		if !stream {
			write(response)
			return
		}
		for seq, event := range []map[string]any{
			{"type": "response.created", "response": map[string]any{"id": "resp_tool_sdk", "object": "response", "created_at": 1, "status": "in_progress", "model": modelName, "output": []any{}}},
			{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "fc_sdk", "type": "function_call", "call_id": "call_sdk", "name": "echo", "arguments": "", "status": "in_progress"}},
			{"type": "response.function_call_arguments.delta", "item_id": "fc_sdk", "output_index": 0, "delta": `{"value":"example"}`},
			{"type": "response.function_call_arguments.done", "item_id": "fc_sdk", "output_index": 0, "arguments": `{"value":"example"}`},
			{"type": "response.output_item.done", "output_index": 0, "item": item}, {"type": "response.completed", "response": response},
		} {
			event["sequence_number"] = seq
			write(event)
		}
	default:
		call := map[string]any{"id": "call_sdk", "type": "function", "function": map[string]any{"name": "echo", "arguments": `{"value":"example"}`}}
		usage := map[string]any{"prompt_tokens": 10, "completion_tokens": 2, "total_tokens": 12}
		if !stream {
			write(map[string]any{"id": "chat_tool_sdk", "object": "chat.completion", "created": 1, "model": modelName, "choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{call}}, "finish_reason": "tool_calls"}}, "usage": usage})
			return
		}
		call["index"] = 0
		for _, choice := range []map[string]any{{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{call}}, "finish_reason": nil}, {"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}} {
			write(map[string]any{"id": "chat_tool_sdk", "object": "chat.completion.chunk", "created": 1, "model": modelName, "choices": []any{choice}, "usage": usage})
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}
}
