package common

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MoeForge/moego/ojson"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/require"
)

func TestJSONEditsKeepReplayBodyAndOwnReturnedBytes(t *testing.T) {
	const original = `{"model":"original","service_tier":"priority","n":9007199254740993,"decimal":1.000e+99,"schema":{"properties":{"z":{},"a":{}}}}`
	input := []byte(original)
	overrides := map[string]any{"operations": []any{
		map[string]any{"mode": "set", "path": "model", "value": "mapped"},
		map[string]any{"mode": "copy", "from": "schema", "to": "copied"},
	}}
	first, err := ApplyParamOverride(input, overrides, nil)
	require.NoError(t, err)
	second, err := ApplyParamOverride(input, overrides, nil)
	require.NoError(t, err)
	require.Equal(t, first, second, "retrying the original body must produce the same wire request")
	require.Equal(t, original, string(input))
	filtered, err := RemoveDisabledFields(first, dto.ChannelOtherSettings{}, false)
	require.NoError(t, err)
	require.Equal(t, `9007199254740993`, ojson.GetBytes(filtered, "n").Raw)
	require.Equal(t, `1.000e+99`, ojson.GetBytes(filtered, "decimal").Raw)
	require.Equal(t, `{"properties":{"z":{},"a":{}}}`, ojson.GetBytes(filtered, "copied").Raw)
	require.False(t, ojson.GetBytes(filtered, "service_tier").Exists())
	wantFirst, wantFiltered := bytes.Clone(first), bytes.Clone(filtered)
	for i := range input {
		input[i] = 'x'
	}
	require.Equal(t, wantFirst, first, "request storage may be reused after editing returns")
	for i := range first {
		first[i] = 'x'
	}
	require.Equal(t, wantFiltered, filtered, "filter output must outlive the previous edit buffer")
	require.Equal(t, wantFirst, second, "independent retries must not share their output buffer")
}

func TestJSONMemberEditsTreatEscapedKeysAsOneMember(t *testing.T) {
	input := []byte(`{"config":{"a.b":"first","a\u002eb":"last","keep":-0},"n":9007199254740993}`)
	doc := ojson.NewDoc(input)
	require.NoError(t, SetJSONMember(doc, "config", "a.b", "selected"))
	require.Equal(t, `{"config":{"keep":-0,"a.b":"selected"},"n":9007199254740993}`, string(doc.Bytes()))
	require.NoError(t, DeleteJSONMember(doc, `config.a\.b`))
	require.Equal(t, `{"config":{"keep":-0},"n":9007199254740993}`, string(doc.Bytes()))
}

func BenchmarkParamOverrideJSONEdits(b *testing.B) {
	input := []byte(`{"model":"original","messages":[{"role":"user","content":"` + strings.Repeat("x", 64<<10) + `"}],"n":9007199254740993}`)
	overrides := map[string]any{"operations": []any{
		map[string]any{"mode": "set", "path": "model", "value": "mapped"},
		map[string]any{"mode": "set", "path": "temperature", "value": 0.5},
		map[string]any{"mode": "set", "path": "stream", "value": true},
	}}
	b.ReportAllocs()
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ApplyParamOverride(input, overrides, nil); err != nil {
			b.Fatal(err)
		}
	}
}
