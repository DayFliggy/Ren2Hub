package reasoning

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestModelPermissionPatternPreservesExistingTokenAllowLists(t *testing.T) {
	for name, expected := range map[string]string{
		"gemini-2.5-flash-lite-thinking-1024": "gemini-2.5-flash-lite-thinking-*",
		"gemini-2.5-flash-thinking-4096":      "gemini-2.5-flash-thinking-*",
		"gemini-2.5-pro-thinking-1024":        "gemini-2.5-pro-thinking-*",
		"gemini-2.5-flash-lite":               "gemini-2.5-flash-lite",
		"gpt-4-gizmo-personal":                "gpt-4-gizmo-*",
		"gpt-4o-gizmo-personal":               "gpt-4o-gizmo-*",
		"vendor/gpt-4-gizmo-personal":         "vendor/gpt-4-gizmo-personal",
		"gpt-5.4":                             "gpt-5.4",
	} {
		assert.Equal(t, expected, ModelPermissionPattern(name), name)
	}
}
