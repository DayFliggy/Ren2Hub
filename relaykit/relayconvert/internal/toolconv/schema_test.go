package toolconv

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/convmeta"
	kitutil "github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// geminiFunctionSchemaRequest is a Gemini request declaring one function whose
// parameters use the Gemini OpenAPI subset.
func geminiFunctionSchemaRequest(t *testing.T, parameters string) *dto.GeminiChatRequest {
	t.Helper()
	tools, err := kitutil.Marshal([]map[string]any{{
		"functionDeclarations": []map[string]any{{
			"name":        "lookup",
			"description": "Look up a city",
			"parameters":  json.RawMessage(parameters),
		}},
	}})
	require.NoError(t, err)
	return &dto.GeminiChatRequest{
		Contents: []dto.GeminiChatContent{{Role: "user", Parts: []dto.GeminiPart{{Text: "hi"}}}},
		Tools:    tools,
	}
}

// attachGeminiFunctionSchema converts one Gemini declaration to a non-Gemini
// target and returns the parameters the target received.
func attachGeminiFunctionSchema(t *testing.T, parameters string, target types.RelayFormat) (map[string]any, []types.ConversionDiagnostic) {
	t.Helper()
	_, set, err := ExtractRequest(types.RelayFormatGemini, target, geminiFunctionSchemaRequest(t, parameters))
	require.NoError(t, err)

	var request any
	switch target {
	case types.RelayFormatOpenAI:
		request = &dto.GeneralOpenAIRequest{Model: "gpt-4o", Messages: []dto.Message{{Role: "user", Content: "hi"}}}
	case types.RelayFormatOpenAIResponses:
		request = &dto.OpenAIResponsesRequest{Model: "gpt-5", Input: json.RawMessage(`"hi"`)}
	case types.RelayFormatClaude:
		request = &dto.ClaudeRequest{Model: "claude-sonnet-4-5", MaxTokens: kitutil.GetPointer(uint(64)), Messages: []dto.ClaudeMessage{{Role: "user", Content: "hi"}}}
	default:
		t.Fatalf("unsupported target %s", target)
	}

	out, diagnostics, err := AttachRequest(target, request, set, &convmeta.Options{})
	require.NoError(t, err)
	require.NotNil(t, out)
	return targetFunctionParameters(t, target, out), diagnostics
}

func targetFunctionParameters(t *testing.T, target types.RelayFormat, request any) map[string]any {
	t.Helper()
	switch target {
	case types.RelayFormatOpenAI:
		tools := request.(*dto.GeneralOpenAIRequest).Tools
		require.Len(t, tools, 1)
		parameters, ok := tools[0].Function.Parameters.(map[string]any)
		require.True(t, ok)
		return parameters
	case types.RelayFormatOpenAIResponses:
		var tools []struct {
			Parameters map[string]any `json:"parameters"`
		}
		require.NoError(t, kitutil.Unmarshal(request.(*dto.OpenAIResponsesRequest).Tools, &tools))
		require.Len(t, tools, 1)
		return tools[0].Parameters
	case types.RelayFormatClaude:
		tools, err := kitutil.Any2Type[[]dto.Tool](request.(*dto.ClaudeRequest).Tools)
		require.NoError(t, err)
		require.Len(t, tools, 1)
		return tools[0].InputSchema
	default:
		t.Fatalf("unsupported target %s", target)
		return nil
	}
}

func schemaDiagnostics(diagnostics []types.ConversionDiagnostic) []types.ConversionDiagnostic {
	var found []types.ConversionDiagnostic
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == "gemini_schema_keyword_dropped" {
			found = append(found, diagnostic)
		}
	}
	return found
}

// TestGeminiOpenAPISchemaBecomesJSONSchemaForNonGeminiTargets covers the
// rewrite of Gemini's OpenAPI subset (uppercase types, nullable,
// propertyOrdering) into JSON Schema for Chat, Claude, and Responses.
func TestGeminiOpenAPISchemaBecomesJSONSchemaForNonGeminiTargets(t *testing.T) {
	t.Parallel()

	const parameters = `{
		"type": "OBJECT",
		"properties": {
			"city": {"type": "STRING"},
			"unit": {"type": "STRING", "nullable": true, "enum": ["c", "f"]},
			"limit": {"type": "INTEGER", "nullable": true},
			"days": {
				"type": "ARRAY",
				"propertyOrdering": ["name"],
				"items": {"type": "OBJECT", "properties": {"name": {"type": "STRING"}}, "required": ["name"]}
			}
		},
		"required": ["city"],
		"propertyOrdering": ["city", "unit", "limit", "days"]
	}`

	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		t.Run(string(target), func(t *testing.T) {
			converted, diagnostics := attachGeminiFunctionSchema(t, parameters, target)

			assert.Equal(t, "object", converted["type"])
			properties, ok := converted["properties"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "string", properties["city"].(map[string]any)["type"])
			assert.Equal(t, []any{"string", "null"}, properties["unit"].(map[string]any)["type"])
			assert.Equal(t, []any{"c", "f", nil}, properties["unit"].(map[string]any)["enum"])
			assert.Equal(t, []any{"integer", "null"}, properties["limit"].(map[string]any)["type"])

			days := properties["days"].(map[string]any)
			assert.NotContains(t, days, "propertyOrdering")
			items, ok := days["items"].(map[string]any)
			require.True(t, ok, "items must recurse into the nested object")
			assert.Equal(t, "object", items["type"])
			nested, ok := items["properties"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, "string", nested["name"].(map[string]any)["type"])

			found := schemaDiagnostics(diagnostics)
			require.Len(t, found, 1)
			assert.Equal(t, "tools[0]", found[0].Path)
			assert.Equal(t, types.ConversionDiagnosticWarning, found[0].Severity)
			assert.Contains(t, found[0].Message, "propertyOrdering")
			assert.Equal(t, string(types.RelayFormatGemini), string(found[0].From))
			assert.Equal(t, string(target), string(found[0].To))
		})
	}
}

// TestGeminiOpenAPISchemaWithEquivalentKeywordsHasNoDiagnostic keeps the
// rewrite quiet when every keyword has a JSON Schema equivalent.
func TestGeminiOpenAPISchemaWithEquivalentKeywordsHasNoDiagnostic(t *testing.T) {
	t.Parallel()

	const parameters = `{
		"type": "OBJECT",
		"properties": {"city": {"type": "STRING", "description": "City name"}, "limit": {"type": "INTEGER"}, "exact": {"type": "NUMBER"}, "open": {"type": "BOOLEAN"}},
		"required": ["city"]
	}`

	for _, target := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		t.Run(string(target), func(t *testing.T) {
			converted, diagnostics := attachGeminiFunctionSchema(t, parameters, target)
			assert.Empty(t, schemaDiagnostics(diagnostics))
			assert.Equal(t, "object", converted["type"])
			properties := converted["properties"].(map[string]any)
			assert.Equal(t, "string", properties["city"].(map[string]any)["type"])
			assert.Equal(t, "integer", properties["limit"].(map[string]any)["type"])
			assert.Equal(t, "number", properties["exact"].(map[string]any)["type"])
			assert.Equal(t, "boolean", properties["open"].(map[string]any)["type"])
		})
	}
}

// TestGeminiOpenAPISchemaAnyOfRecursesAndStaysNullable checks the anyOf branch
// and the nullable representation JSON Schema uses when no single type exists.
func TestGeminiOpenAPISchemaAnyOfRecursesAndStaysNullable(t *testing.T) {
	t.Parallel()

	const parameters = `{
		"type": "OBJECT",
		"properties": {
			"value": {"anyOf": [{"type": "STRING"}, {"type": "ARRAY", "items": {"type": "INTEGER"}}], "nullable": true}
		}
	}`

	converted, diagnostics := attachGeminiFunctionSchema(t, parameters, types.RelayFormatOpenAI)
	assert.Empty(t, schemaDiagnostics(diagnostics))
	value := converted["properties"].(map[string]any)["value"].(map[string]any)
	variants, ok := value["anyOf"].([]any)
	require.True(t, ok)
	require.Len(t, variants, 3)
	assert.Equal(t, "string", variants[0].(map[string]any)["type"])
	array := variants[1].(map[string]any)
	assert.Equal(t, "array", array["type"])
	assert.Equal(t, "integer", array["items"].(map[string]any)["type"])
	assert.Equal(t, map[string]any{"type": "null"}, variants[2])
}

// TestGeminiTargetKeepsTheOpenAPISchemaSubset pins that a Gemini target keeps
// today's behaviour: the cleaning path runs, not the JSON Schema rewrite.
func TestGeminiTargetKeepsTheOpenAPISchemaSubset(t *testing.T) {
	t.Parallel()

	const parameters = `{"type": "OBJECT", "properties": {"city": {"type": "STRING"}}, "propertyOrdering": ["city"]}`
	_, set, err := ExtractRequest(types.RelayFormatGemini, types.RelayFormatGemini, geminiFunctionSchemaRequest(t, parameters))
	require.NoError(t, err)
	out, diagnostics, err := AttachRequest(types.RelayFormatGemini, &dto.GeminiChatRequest{}, set, &convmeta.Options{})
	require.NoError(t, err)
	require.NotNil(t, out)

	declarations := out.(*dto.GeminiChatRequest).GetTools()
	require.Len(t, declarations, 1)
	functions, err := kitutil.Any2Type[[]dto.FunctionRequest](declarations[0].FunctionDeclarations)
	require.NoError(t, err)
	require.Len(t, functions, 1)
	converted, ok := functions[0].Parameters.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "OBJECT", converted["type"])
	assert.Equal(t, []any{"city"}, converted["propertyOrdering"])
	assert.Empty(t, schemaDiagnostics(diagnostics))
}

// TestStrictToolPolicyRejectsTheDroppedGeminiSchemaKeywords holds the local
// loss policy: safe passes the diagnosed loss, strict rejects it.
func TestStrictToolPolicyRejectsTheDroppedGeminiSchemaKeywords(t *testing.T) {
	t.Parallel()

	const parameters = `{"type": "OBJECT", "properties": {"city": {"type": "STRING"}}, "propertyOrdering": ["city"]}`
	_, set, err := ExtractRequest(types.RelayFormatGemini, types.RelayFormatOpenAI, geminiFunctionSchemaRequest(t, parameters))
	require.NoError(t, err)

	target := func() *dto.GeneralOpenAIRequest {
		return &dto.GeneralOpenAIRequest{Model: "gpt-4o", Messages: []dto.Message{{Role: "user", Content: "hi"}}}
	}
	out, diagnostics, err := AttachRequest(types.RelayFormatOpenAI, target(), set, &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicySafe})
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Len(t, schemaDiagnostics(diagnostics), 1)

	_, diagnostics, err = AttachRequest(types.RelayFormatOpenAI, target(), set, &convmeta.Options{ToolLossPolicy: types.ConversionLossPolicyStrict})
	require.Error(t, err)
	var loss *types.ConversionLossError
	require.ErrorAs(t, err, &loss)
	require.Len(t, loss.Diagnostics, 1)
	assert.Equal(t, "gemini_schema_keyword_dropped", loss.Diagnostics[0].Code)
	assert.True(t, hasDiagnosticCode(diagnostics, "gemini_schema_keyword_dropped"))
}
