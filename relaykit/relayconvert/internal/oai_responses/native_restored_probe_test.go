//go:build known_upstream_restored

package oairesponses

import (
	"context"
	"encoding/json"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRestoredProbeResponsesPromptCacheOptions(t *testing.T) {
	got, err := ResponsesRequestToChatCompletionsRequest(context.Background(), &dto.OpenAIResponsesRequest{Model: "gpt-test", Input: json.RawMessage(`"hello"`), PromptCacheOptions: json.RawMessage(`{"ttl":"12h"}`)})
	require.NoError(t, err)
	assert.JSONEq(t, `{"ttl":"12h"}`, string(got.PromptCacheOptions))
}
