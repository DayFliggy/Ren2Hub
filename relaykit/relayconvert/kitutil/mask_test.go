package kitutil

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMaskSensitiveInfoRedactsGoogleCredentialsInUpstreamErrors(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"AIza" + strings.Repeat("a", 35),
		"AQ.Ab" + strings.Repeat("a", 40),
		"AQ.Ab" + strings.Repeat("a0_-", 16),
	} {
		t.Run(key[:4], func(t *testing.T) {
			assert.Equal(t, "Rejected credential ***.", MaskSensitiveInfo("Rejected credential "+key+"."))
			assert.Equal(t, `{"message":"Invalid key ***"}`, MaskSensitiveInfo(`{"message":"Invalid key `+key+`"}`))
			assert.Equal(t, "api_key:***", MaskSensitiveInfo("api_key:"+key))
		})
	}
	assert.Equal(t, "Gemini auth key format AQ.", MaskSensitiveInfo("Gemini auth key format AQ."))
}
