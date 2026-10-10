package reasoning

import (
	"regexp"
	"strings"
	"unicode"
)

// dottedClaudeVersion matches the dotted version OpenRouter-style names use,
// such as claude-opus-5.5 or claude-fable-5.1, followed by any suffix.
var dottedClaudeVersion = regexp.MustCompile(`^(claude-[a-z]+-\d+)\.(\d+)`)

// canonicalClaudeModelID reduces a provider-qualified Claude model name to the
// Anthropic model ID the capability table is keyed on, so a Bedrock or
// namespaced channel name gets the same thinking and tool_choice rendering as
// the plain name. Adapted from Sub2API normalizeEffortModelID: it keeps the
// last path segment, drops a cloud region prefix, "anthropic." and a trailing
// -YYYYMMDD date, and writes a dotted version such as 5.5 as 5-5 whatever
// follows it (a date, :thinking, an effort tail). A Bedrock version suffix
// such as -v1:0 is left in place; prefix matching ignores it.
func canonicalClaudeModelID(model string) string {
	id := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
		id = strings.TrimSpace(id[slash+1:])
	}
	for _, prefix := range []string{"us.", "eu.", "apac.", "jp.", "au.", "us-gov.", "global."} {
		id = strings.TrimPrefix(id, prefix)
	}
	id = strings.TrimPrefix(id, "anthropic.")
	id = dottedClaudeVersion.ReplaceAllString(id, "$1-$2")
	if len(id) >= 9 && id[len(id)-9] == '-' && strings.IndexFunc(id[len(id)-8:], func(r rune) bool { return !unicode.IsDigit(r) }) < 0 {
		id = id[:len(id)-9]
	}
	return id
}
