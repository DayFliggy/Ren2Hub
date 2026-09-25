package modelmapping

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParseAndResolveModelMapping(t *testing.T) {
	for _, test := range []struct {
		name, raw, model, want string
		mapped                 bool
	}{
		{"exact modifier wins", `{"a@effort:high":"exact","a":"base"}`, "a@effort:high", "exact", true},
		{"base mapping", `{"a":"b","b":"c@effort:max"}`, "a@effort:high", "c@effort:max", true},
		{"base self mapping", `{"a":"a"}`, "a@effort:high", "a", true},
		{"empty", "", "model", "model", false},
		{"unmapped", `{"a":"b"}`, "model", "model", false},
		{"chain", `{"a":"b","b":"c"}`, "a", "c", true},
		{"self", `{"a":"a"}`, "a", "a", false},
		{"chain self", `{"a":"b","b":"b"}`, "a", "b", true},
		{"empty target", `{"a":""}`, "a", "a", false},
		{"literal suffix", `{"a":"real-openai-compact"}`, "a", "real-openai-compact", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			mapping, err := Parse(test.raw)
			require.NoError(t, err)
			actual, mapped, err := Resolve(test.model, mapping)
			require.NoError(t, err)
			assert.Equal(t, test.want, actual)
			assert.Equal(t, test.mapped, mapped)
		})
	}
	_, err := Parse(`{"a":1}`)
	require.Error(t, err)
	_, _, err = Resolve("a", map[string]string{"a": "b", "b": "a"})
	require.EqualError(t, err, "model_mapping_contains_cycle")
}
