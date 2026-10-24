package constant

import (
	"errors"
	"strings"

	appconstant "github.com/QuantumNous/new-api/constant"
)

// NativeCountChannelType is the shared admission/dispatch capability rule.
// Only ordinary provider API-key channels have a native count contract.
func NativeCountChannelType(path string) int {
	if path == "/v1/messages/count_tokens" {
		return appconstant.ChannelTypeAnthropic
	}
	if (strings.HasPrefix(path, "/v1/models/") || strings.HasPrefix(path, "/v1beta/models/")) && strings.HasSuffix(path, ":countTokens") {
		return appconstant.ChannelTypeGemini
	}
	return 0
}

func SafeGeminiModelSegment(model string) bool {
	if model == "" || model == "." || model == ".." {
		return false
	}
	for _, r := range model {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// ParseGeminiModelAction prevents unknown actions and encoded separators from
// being silently rewritten by the generation adaptor.
func ParseGeminiModelAction(path string) (string, string, error) {
	var value string
	switch {
	case strings.HasPrefix(path, "/v1/models/"):
		value = strings.TrimPrefix(path, "/v1/models/")
	case strings.HasPrefix(path, "/v1beta/models/"):
		value = strings.TrimPrefix(path, "/v1beta/models/")
	default:
		return "", "", errors.New("invalid Gemini model action path")
	}
	model, action, found := strings.Cut(value, ":")
	if !found || !SafeGeminiModelSegment(model) {
		return "", "", errors.New("invalid Gemini model path segment")
	}
	switch action {
	case "generateContent", "streamGenerateContent", "embedContent", "batchEmbedContents", "predict", "countTokens":
		return model, action, nil
	default:
		return "", "", errors.New("unsupported Gemini action")
	}
}
