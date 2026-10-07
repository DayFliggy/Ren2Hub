package dto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Providers such as OpenRouter report usage.cost as a number, while some
// compatible gateways send a numeric string (AxonHub c9e866c). Ren2Hub only
// passes the value through, so either form must survive decoding, stream usage
// merging and re-encoding unchanged without disturbing the token counts.
func TestUsageCostKeepsProviderNumberOrString(t *testing.T) {
	for _, test := range []struct{ name, cost string }{
		{"number", `0.0001514`},
		{"numeric string", `"0.0001514"`},
		{"exponent string", `"1.514e-4"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14,"prompt_tokens_details":{"cached_tokens":3},"cost":` + test.cost + `}`
			var usage Usage
			require.NoError(t, json.Unmarshal([]byte(body), &usage))
			assert.Equal(t, 10, usage.PromptTokens)
			assert.Equal(t, 4, usage.CompletionTokens)
			assert.Equal(t, 14, usage.TotalTokens)
			assert.Equal(t, 3, usage.PromptTokensDetails.CachedTokens)

			merged := MergeUsageNonZero(&Usage{}, &usage)
			for _, value := range []Usage{usage, *merged} {
				encoded, err := json.Marshal(value)
				require.NoError(t, err)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(encoded, &fields))
				assert.JSONEq(t, test.cost, string(fields["cost"]))
				assert.JSONEq(t, "10", string(fields["prompt_tokens"]))
			}
		})
	}
}
