package relayconvert

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// geminiSchemaMeta targets a Gemini upstream that does or does not accept
// parametersJsonSchema and responseJsonSchema.
func geminiSchemaMeta(policy types.ConversionLossPolicy, jsonSchema bool) *convmeta.Values {
	return &convmeta.Values{
		UpstreamModelName:   "gemini-3.7-flash",
		ChannelMetaAttached: true,
		Options: &convmeta.Options{
			ConversionPolicy: policy,
			Gemini:           convmeta.GeminiOptions{SupportsJSONSchema: func(string) bool { return jsonSchema }},
		},
	}
}

func chatSchemaRequest(t *testing.T, parameters string, responseSchema string) *dto.GeneralOpenAIRequest {
	t.Helper()
	body := `{"model":"gemini-3.7-flash","messages":[{"role":"user","content":"hi"}],` +
		`"tools":[{"type":"function","function":{"name":"set_mode","parameters":` + parameters + `}}]`
	if responseSchema != "" {
		body += `,"response_format":{"type":"json_schema","json_schema":{"name":"result","schema":` + responseSchema + `}}`
	}
	request := &dto.GeneralOpenAIRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(body+`}`), request))
	return request
}

// A tool or structured output schema that Gemini's typed Schema cannot hold
// reaches an upstream that accepts JSON Schema unchanged, with no loss under
// any policy (NewAPI c10780e05).
func TestGeminiJSONSchemaUpstreamReceivesSchemasUnchanged(t *testing.T) {
	const parameters = `{"type":"object","properties":{"mode":{"const":"fast"},"verbose":{"type":"boolean","enum":[true]}},"required":["mode"]}`
	const responseSchema = `{"type":"object","properties":{"value":{"type":["string","null"]}}}`

	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict, types.ConversionLossPolicyAllow} {
		t.Run(string(policy), func(t *testing.T) {
			result, err := ConvertRequest(context.Background(), geminiSchemaMeta(policy, true), types.RelayFormatGemini, chatSchemaRequest(t, parameters, responseSchema))
			require.NoError(t, err)
			for _, diagnostic := range result.Diagnostics {
				assert.NotContains(t, []string{"gemini_schema_keyword_dropped", "schema_field_not_portable", "schema_union_not_portable"}, diagnostic.Code)
			}

			encoded := marshaledField(t, result.Value)
			declaration := gjson.Get(encoded, "tools.0.functionDeclarations.0")
			assert.False(t, declaration.Get("parameters").Exists())
			assert.JSONEq(t, parameters, declaration.Get("parametersJsonSchema").Raw)
			assert.JSONEq(t, responseSchema, gjson.Get(encoded, "generationConfig.responseJsonSchema").Raw)
			assert.False(t, gjson.Get(encoded, "generationConfig.responseSchema").Exists())
		})
	}
}

// Gemini API v1 has no parametersJsonSchema: the schema is cleaned to the
// typed Schema, which also drops non-string enum values that Gemini rejects
// with a 400 (NewAPI 4b13d9388), and the loss follows the local policy.
func TestGeminiTypedSchemaUpstreamCleansSchemasUnderLossPolicy(t *testing.T) {
	const parameters = `{"type":"object","properties":{"verbose":{"type":"boolean","enum":[true]},"color":{"type":"string","enum":["red"]}}}`

	for _, policy := range []types.ConversionLossPolicy{types.ConversionLossPolicySafe, types.ConversionLossPolicyStrict} {
		_, err := ConvertRequest(context.Background(), geminiSchemaMeta(policy, false), types.RelayFormatGemini, chatSchemaRequest(t, parameters, ""))
		require.ErrorContains(t, err, "tools[0].function.parameters.properties.verbose.enum", string(policy))
	}

	result, err := ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicyAllow, false), types.RelayFormatGemini, chatSchemaRequest(t, parameters, ""))
	require.NoError(t, err)
	assert.NotNil(t, findDiagnostic(result.Diagnostics, "gemini_schema_keyword_dropped"))

	encoded := marshaledField(t, result.Value)
	properties := gjson.Get(encoded, "tools.0.functionDeclarations.0.parameters.properties")
	assert.JSONEq(t, `{"type":"BOOLEAN"}`, properties.Get("verbose").Raw)
	assert.JSONEq(t, `{"type":"STRING","enum":["red"]}`, properties.Get("color").Raw)
	assert.False(t, gjson.Get(encoded, "tools.0.functionDeclarations.0.parametersJsonSchema").Exists())
}

// additionalProperties and $schema alone keep the typed Schema on every
// upstream, so agent and OpenAI strict tool schemas are sent as before.
func TestGeminiSchemaWithOnlyAdditionalPropertiesStaysTyped(t *testing.T) {
	const parameters = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":false,"properties":{"q":{"type":"string"}}}`

	result, err := ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicyAllow, true), types.RelayFormatGemini, chatSchemaRequest(t, parameters, ""))
	require.NoError(t, err)
	encoded := marshaledField(t, result.Value)
	assert.JSONEq(t, `{"type":"OBJECT","properties":{"q":{"type":"STRING"}}}`, gjson.Get(encoded, "tools.0.functionDeclarations.0.parameters").Raw)
	assert.False(t, gjson.Get(encoded, "tools.0.functionDeclarations.0.parametersJsonSchema").Exists())
}

// The Responses converter applies the same rule to text.format.schema.
func TestGeminiJSONSchemaUpstreamReceivesResponsesOutputSchemaUnchanged(t *testing.T) {
	const schema = `{"type":"object","properties":{"kind":{"oneOf":[{"const":"a"},{"const":"b"}]}}}`
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-3.7-flash","input":"hi","text":{"format":{"type":"json_schema","name":"result","schema":`+schema+`}}}`), request))

	result, err := ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicySafe, true), types.RelayFormatGemini, request)
	require.NoError(t, err)
	encoded := marshaledField(t, result.Value)
	assert.JSONEq(t, schema, gjson.Get(encoded, "generationConfig.responseJsonSchema").Raw)

	_, err = ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicyStrict, true), types.RelayFormatGemini, request)
	require.NoError(t, err)

	// responseSchema keeps oneOf as written, so on Gemini API v1 the loss is
	// the converter's warning: safe sends it, strict rejects it.
	result, err = ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicySafe, false), types.RelayFormatGemini, request)
	require.NoError(t, err)
	dropped := findDiagnostic(result.Diagnostics, "gemini_schema_keyword_dropped")
	require.NotNil(t, dropped)
	assert.Equal(t, "text.format.schema", dropped.Path)
	assert.Equal(t, types.ConversionDiagnosticWarning, dropped.Severity)
	assert.Contains(t, dropped.Message, "oneOf")
	_, err = ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicyStrict, false), types.RelayFormatGemini, request)
	require.ErrorContains(t, err, "text.format.schema: Gemini response schema cannot carry")
}

// Responses function tools take the same parametersJsonSchema decision as
// Chat tools: the Responses converter itself declares no tools.
func TestGeminiJSONSchemaUpstreamReceivesResponsesToolSchemaUnchanged(t *testing.T) {
	const parameters = `{"type":"object","properties":{"mode":{"const":"fast"}}}`
	request := &dto.OpenAIResponsesRequest{}
	require.NoError(t, kitutil.Unmarshal([]byte(`{"model":"gemini-3.7-flash","input":"hi","tools":[{"type":"function","name":"set_mode","parameters":`+parameters+`}]}`), request))

	result, err := ConvertRequest(context.Background(), geminiSchemaMeta(types.ConversionLossPolicySafe, true), types.RelayFormatGemini, request)
	require.NoError(t, err)
	encoded := marshaledField(t, result.Value)
	declarations := gjson.Get(encoded, "tools.#.functionDeclarations|@flatten")
	require.Len(t, declarations.Array(), 1)
	assert.JSONEq(t, parameters, declarations.Get("0.parametersJsonSchema").Raw)
	assert.False(t, declarations.Get("0.parameters").Exists())
}
