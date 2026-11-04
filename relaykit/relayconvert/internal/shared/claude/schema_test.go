package claude

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchemaDefaultsKeepRawNumbersAndPropertyOrder(t *testing.T) {
	input := json.RawMessage(`{"properties":{"z":{"default":9007199254740993},"a":{"default":1.00e+99}},"type":null}`)
	original := bytes.Clone(input)
	schema, ok := FunctionParametersToInputSchema(input)
	require.True(t, ok)
	want := `{"properties":{"z":{"default":9007199254740993},"a":{"default":1.00e+99}},"type":"object"}`
	require.Equal(t, want, string(schema))
	require.Equal(t, original, []byte(input))
	for i := range input {
		input[i] = 'x'
	}
	require.Equal(t, want, string(schema))
}
