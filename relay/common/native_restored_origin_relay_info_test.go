//go:build known_upstream_restored

package common

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRestoredProbeRelayInfoToStringMasksURLCredentials(t *testing.T) {
	info := &RelayInfo{
		RequestURLPath: "/v1/responses?api_key=client-secret&X-Goog-Credential=signed-secret&model=gpt-test",
	}

	logged := info.ToString()

	assert.False(t, strings.Contains(logged, "client-secret"), "restored_relay_info:client_secret_absent")
	assert.False(t, strings.Contains(logged, "signed-secret"), "restored_relay_info:signed_secret_absent")
	assert.True(t, strings.Contains(logged, "model=gpt-test"), "restored_relay_info:ordinary_model_preserved")
	assert.True(t, strings.Contains(logged, "api_key=%2A%2A%2Amasked%2A%2A%2A"), "restored_relay_info:api_key_mask_present")
	assert.True(t, strings.Contains(info.RequestURLPath, "api_key=client-secret"), "restored_relay_info:logging_must_not_change_relayed_url")
}
