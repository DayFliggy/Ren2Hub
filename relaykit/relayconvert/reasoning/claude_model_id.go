package reasoning

import (
	"strings"
	"unicode"
)

// canonicalClaudeModelID reduces a provider-qualified Claude model name to the
// Anthropic model ID the capability table is keyed on, so a Bedrock or
// namespaced channel name gets the same thinking and tool_choice rendering as
// the plain name. Adapted from Sub2API normalizeEffortModelID: it drops a
// "namespace/" prefix, a cloud region prefix, "anthropic." and a trailing
// -YYYYMMDD date, and maps the dotted OpenRouter 5.5 IDs. A Bedrock version
// suffix such as -v1:0 is left in place; prefix matching ignores it.
func canonicalClaudeModelID(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	id = strings.TrimPrefix(id, "models/")
	if slash := strings.IndexByte(id, '/'); slash >= 0 {
		id = strings.TrimPrefix(strings.TrimSpace(id[slash+1:]), "models/")
	}
	for _, prefix := range []string{"us.", "eu.", "apac.", "jp.", "au.", "us-gov.", "global."} {
		id = strings.TrimPrefix(id, prefix)
	}
	id = strings.TrimPrefix(id, "anthropic.")
	switch id {
	case "claude-opus-5.5":
		id = "claude-opus-5-5"
	case "claude-sonnet-5.5":
		id = "claude-sonnet-5-5"
	}
	if len(id) >= 9 && id[len(id)-9] == '-' && strings.IndexFunc(id[len(id)-8:], func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
		id = id[:len(id)-9]
	}
	return id
}
