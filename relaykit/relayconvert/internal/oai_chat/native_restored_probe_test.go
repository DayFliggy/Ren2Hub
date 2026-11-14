//go:build known_upstream_restored

package oaichat

import (
	"context"
	"encoding/json"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRestoredProbeChatCompletionsRequestToResponsesRequestPreservesPromptCacheOptions(t *testing.T) {
	options := json.RawMessage(`{"ttl":"12h"}`)
	got, err := ChatCompletionsRequestToResponsesRequest(context.Background(), &dto.GeneralOpenAIRequest{
		Model:              "gpt-test",
		Messages:           []dto.Message{{Role: "user", Content: "hello"}},
		PromptCacheOptions: options,
	})
	require.NoError(t, err)
	assert.JSONEq(t, string(options), string(got.PromptCacheOptions))
}
