package relaycapability

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTencentCredentialSurface(t *testing.T) {
	for _, tc := range []struct {
		name        string
		credentials []string
		native      bool
	}{
		{"unknown", nil, false},
		{"TC3", []string{"test-app|test-secret|test-key"}, false},
		{"TokenHub", []string{"test-key-a", "test-key-b"}, true},
		{"mixed", []string{"test-key-a", "test-app|test-secret|test-key"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition, ok := DescribeForCredentials(constant.APITypeTencent, "hunyuan", tc.credentials)
			require.True(t, ok)
			surface, ok := definition.Resolve(types.RelayFormatOpenAI)
			require.True(t, ok)
			assert.Equal(t, tc.native, surface.Native)
			assert.Equal(t, tc.native, surface.TextProjection == nil)
		})
	}
}
