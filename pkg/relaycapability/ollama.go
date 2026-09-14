package relaycapability

// OllamaChatProjection is shared by candidate planning and the final Chat DTO
// conversion. It describes only fields actually consumed by /api/chat.
func OllamaChatProjection() *TextProjection {
	return &TextProjection{
		ID: "ollama_chat", Roles: []string{"system", "user", "assistant", "tool"}, StrictRoles: true,
		Images: true, ConvertedInputs: true,
		Fields: []string{"model", "messages", "stream", "stream_options", "max_tokens", "max_completion_tokens",
			"temperature", "top_p", "top_k", "frequency_penalty", "presence_penalty", "seed", "stop",
			"tools", "tool_choice", "parallel_tool_calls", "response_format", "think", "reasoning", "reasoning_effort"},
		MessageFields: map[string][]string{
			"assistant": {"tool_calls", "reasoning_content", "reasoning"},
			"tool":      {"tool_call_id", "name"},
		},
		UnsupportedFeatures: map[string]string{
			"forced_tools":     "ollama cannot enforce required or named tool selection",
			"serial_tools":     "ollama cannot enforce serial tool execution",
			"strict_tools":     "ollama does not implement strict function schema enforcement",
			"legacy_functions": "ollama requires function declarations in tools",
		},
	}
}
