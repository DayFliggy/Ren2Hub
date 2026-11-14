//go:build known_upstream_restored

package common

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRestoredProbeSanitizeURLForLogDoesNotRestoreMalformedCredentials(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input string
		want  string
	}{
		{"invalid value escape", "/path?api_key=secret%", "/path"},
		{"semicolon", "/path?key=secret;other=value", "/path"},
		{"invalid key escape", "/path?%ZZ=secret&view=full", "/path?view=full"},
		{"preserve valid pairs", "/path?api_key=secret%&view=full", "/path?view=full"},
		{"mask remaining credential", "/path?broken=%ZZ&token=secret&view=full", "/path?token=%2A%2A%2Amasked%2A%2A%2A&view=full"},
		{"invalid path escape", "/%ZZ?token=secret", "***invalid-url***"},
		{"invalid control byte", "/path\n?token=secret", "***invalid-url***"},
		{"raw fragment in credential", "/path?key=#secret", "/path?key=%2A%2A%2Amasked%2A%2A%2A"},
		{"URL credentials", "https://user:secret@example.test/path?view=full", "https://example.test/path?view=full"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeURLForLog(tc.input)
			assert.Equal(t, tc.want, got)
			assert.NotContains(t, got, "secret")
		})
	}
}
