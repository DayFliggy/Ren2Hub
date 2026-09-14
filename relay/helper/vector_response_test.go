package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateEmbeddingResponseContracts(t *testing.T) {
	for _, test := range []struct {
		name, body string
		count      int
		valid      bool
	}{
		{"zero_component", `{"data":[{"index":0,"embedding":[0]}]}`, 1, true},
		{"base64_component", `{"data":[{"index":0,"embedding":"AACAPw=="}]}`, 1, true},
		{"null_component", `{"data":[{"index":0,"embedding":[null]}]}`, 1, false},
		{"missing_index", `{"data":[{"embedding":[1]}]}`, 1, false},
		{"duplicate_index", `{"data":[{"index":0,"embedding":[1]},{"index":0,"embedding":[2]}]}`, 2, false},
		{"partial_batch", `{"data":[{"index":0,"embedding":[1]}]}`, 2, false},
		{"dimension_mismatch", `{"data":[{"index":0,"embedding":[1]},{"index":1,"embedding":[2,3]}]}`, 2, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.valid, ValidateEmbeddingResponse([]byte(test.body), test.count) == nil)
		})
	}
}

func TestValidateRerankResponseContracts(t *testing.T) {
	for _, body := range []string{
		`{"results":[]}`,
		`{"results":[{"index":0}]}`,
		`{"results":[{"relevance_score":0}]}`,
		`{"results":[{"index":-1,"relevance_score":0}]}`,
		`{"results":[{"index":0,"relevance_score":0},{"index":0,"relevance_score":1}]}`,
		`{"results":[{"index":0,"relevance_score":null}]}`,
	} {
		assert.Error(t, ValidateRerankResponse([]byte(body), 2))
	}
	assert.NoError(t, ValidateRerankResponse([]byte(`{"results":[{"index":0,"relevance_score":0}]}`), 2))
}

func TestValidateVectorUsageRejectsNegativeAndInconsistent(t *testing.T) {
	for _, usage := range []*dto.Usage{
		{PromptTokens: -1},
		{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 2},
		{InputTokens: 3, OutputTokens: 4, TotalTokens: 5},
		{PromptTokens: 1, CompletionTokens: 1, TotalTokens: 2, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: -1}},
	} {
		require.Error(t, ValidateVectorUsage(usage))
	}
	require.NoError(t, ValidateVectorUsage(&dto.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}))
}
