package dto

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeMessageOutputConfigRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "omitted", body: `{"role":"user","content":"hello"}`},
		{name: "effort_only", body: `{"role":"system","content":[],"output_config":{"effort":"high"}}`},
		{name: "opaque_config", body: `{"role":"system","content":[],"output_config":{"effort":"medium","extension":{"count":9007199254740993,"enabled":false,"budget":0}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var message ClaudeMessage
			require.NoError(t, kitutil.UnmarshalJsonStr(tc.body, &message))
			encoded, err := kitutil.Marshal(message)
			require.NoError(t, err)
			var before, after map[string]json.RawMessage
			require.NoError(t, kitutil.UnmarshalJsonStr(tc.body, &before))
			require.NoError(t, kitutil.Unmarshal(encoded, &after))
			assert.Equal(t, before, after)
		})
	}
}
