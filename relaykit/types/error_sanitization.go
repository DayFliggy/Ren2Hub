package types

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
)

var errorCredentialPattern = regexp.MustCompile(`(?i)(["']?(?:api[_-]?key|x-api-key|x-goog-api-key|authorization|proxy-authorization|password|passwd|secret|client[_-]?secret|access[_-]?token|refresh[_-]?token|id[_-]?token|token|cookie|set-cookie)["']?\s*[:=]\s*)("(?:\\.|[^"\\])*"|'[^']*'|[^\s,;}\]]+)`)
var errorBearerPattern = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+[^\s,"';}\]]+`)

// BindSensitiveContext only affects public text and protocol projections. The
// original cause and classification remain available to retry/routing logic.
func (e *NewAPIError) BindSensitiveContext(upstreamURLs []string, secrets ...string) {
	if e == nil {
		return
	}
	for _, value := range upstreamURLs {
		if value != "" && !slices.Contains(e.upstreamURLs, value) {
			e.upstreamURLs = append(e.upstreamURLs, value)
		}
	}
	for _, value := range secrets {
		if value != "" && !slices.Contains(e.sensitiveValues, value) {
			e.sensitiveValues = append(e.sensitiveValues, value)
		}
	}
}

// SanitizeText is shared by early logs and client projections. It also handles
// an entire JSON body before any preview truncation can split a credential.
func (e *NewAPIError) SanitizeText(text string, upstreamURLs ...string) string {
	mask := e.textMask(upstreamURLs...)
	if json.Valid([]byte(text)) {
		return string(sanitizeErrorJSON(json.RawMessage(text), mask))
	}
	// OpenRouter appends raw metadata to its message. Replace that whole
	// fragment structurally before masking prose, including credential objects.
	if len(e.Metadata) > 0 {
		text = strings.ReplaceAll(text, string(e.Metadata), string(sanitizeErrorJSON(e.Metadata, mask)))
	}
	return mask(text)
}

func (e *NewAPIError) textMask(upstreamURLs ...string) func(string) string {
	addresses := append(slices.Clone(e.upstreamURLs), upstreamURLs...)
	return func(text string) string {
		for _, secret := range e.sensitiveValues {
			text = strings.ReplaceAll(text, secret, "***")
			encoded, _ := json.Marshal(secret)
			text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "***")
		}
		text = errorBearerPattern.ReplaceAllString(text, "${1} ***")
		text = errorCredentialPattern.ReplaceAllStringFunc(text, func(value string) string {
			parts := errorCredentialPattern.FindStringSubmatch(value)
			if strings.HasPrefix(parts[2], `"`) {
				return parts[1] + `"***"`
			}
			if strings.HasPrefix(parts[2], "'") {
				return parts[1] + "'***'"
			}
			return parts[1] + "***"
		})
		return kitutil.MaskSensitiveInfo(text, addresses...)
	}
}

func sensitiveErrorKey(key string) bool {
	switch strings.ToLower(strings.NewReplacer("-", "", "_", "", " ", "").Replace(key)) {
	case "apikey", "xapikey", "xgoogapikey", "authorization", "proxyauthorization", "password", "passwd", "secret", "clientsecret", "token", "accesstoken", "refreshtoken", "idtoken", "cookie", "setcookie":
		return true
	}
	return false
}

func sanitizeErrorJSON(raw json.RawMessage, mask func(string) string) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if !json.Valid(raw) || decoder.Decode(&value) != nil {
		return json.RawMessage(`"***"`)
	}
	result, err := json.Marshal(sanitizeErrorValue(value, mask))
	if err != nil {
		return json.RawMessage(`"***"`)
	}
	return result
}

func sanitizeErrorValue(value any, mask func(string) string) any {
	switch v := value.(type) {
	case string:
		return mask(v)
	case ErrorCode:
		return ErrorCode(mask(string(v)))
	case json.RawMessage:
		return sanitizeErrorJSON(v, mask)
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if sensitiveErrorKey(key) {
				result[mask(key)] = "***"
			} else {
				result[mask(key)] = sanitizeErrorValue(item, mask)
			}
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = sanitizeErrorValue(item, mask)
		}
		return result
	default:
		return value
	}
}

func (e *NewAPIError) sanitizeOpenAIError(result OpenAIError, upstreamURLs ...string) OpenAIError {
	mask := e.textMask(upstreamURLs...)
	result.Message = e.SanitizeText(result.Message, upstreamURLs...)
	result.Type = mask(result.Type)
	result.Param = mask(result.Param)
	result.Code = sanitizeErrorValue(result.Code, mask)
	result.Metadata = sanitizeErrorJSON(result.Metadata, mask)
	return result
}

// RawError is for internal classification only. Never write it to logs or
// responses; Error and the protocol projections apply the disclosure policy.
func (e *NewAPIError) RawError() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return string(e.errorCode)
	}
	if nested, ok := e.Err.(*NewAPIError); ok {
		return nested.RawError()
	}
	return e.Err.Error()
}

// RawOpenAIError preserves the original provider fields for internal routing,
// retry and billing rules. External consumers must use ToOpenAIError.
func (e *NewAPIError) RawOpenAIError() OpenAIError {
	var result OpenAIError
	switch e.errorType {
	case ErrorTypeOpenAIError:
		if openAIError, ok := e.RelayError.(OpenAIError); ok {
			result = openAIError
		}
	case ErrorTypeClaudeError:
		if claudeError, ok := e.RelayError.(ClaudeError); ok {
			result = OpenAIError{
				Message: e.RawError(),
				Type:    claudeError.Type,
				Param:   "",
				Code:    e.errorCode,
			}
		}
	default:
		result = OpenAIError{
			Message: e.RawError(),
			Type:    string(e.errorType),
			Param:   "",
			Code:    e.errorCode,
		}
	}
	if result.Message == "" {
		result.Message = string(e.errorType)
	}
	return result
}

// ErrOptionWithSensitiveContext preserves disclosure inputs when a semantic
// normalization creates a new error, without replacing its internal fields.
func ErrOptionWithSensitiveContext(source *NewAPIError) NewAPIErrorOptions {
	return func(target *NewAPIError) {
		if source != nil {
			target.BindSensitiveContext(source.upstreamURLs, source.sensitiveValues...)
		}
	}
}
