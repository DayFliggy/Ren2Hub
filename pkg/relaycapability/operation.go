package relaycapability

import "strings"

// LegacyOpenAIOperation distinguishes product operations that share the OpenAI
// DTO but cannot use the chat request/response converters.
func LegacyOpenAIOperation(path string) string {
	switch strings.Split(path, "?")[0] {
	case "/v1/completions":
		return "completion"
	case "/v1/moderations":
		return "moderation"
	case "/v1/edits":
		return "edit"
	default:
		return ""
	}
}
