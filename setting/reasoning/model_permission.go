package reasoning

import "strings"

// ModelPermissionPattern preserves existing token allow-list patterns. These
// aliases express access permissions and must never be used to select a price.
func ModelPermissionPattern(name string) string {
	for _, base := range []string{"gemini-2.5-flash-lite", "gemini-2.5-flash", "gemini-2.5-pro"} {
		if strings.HasPrefix(name, base) {
			if strings.Contains(name, "-thinking-") {
				return base + "-thinking-*"
			}
			break
		}
	}
	if strings.HasPrefix(name, "gpt-4-gizmo") {
		return "gpt-4-gizmo-*"
	}
	if strings.HasPrefix(name, "gpt-4o-gizmo") {
		return "gpt-4o-gizmo-*"
	}
	return name
}
