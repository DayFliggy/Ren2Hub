package service

import (
	"context"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelClientConfigPreservesPermissionsUnknownsAndEnvironmentReferences(t *testing.T) {
	contextLimit, outputLimit := int64(32000), int64(1000)
	toolCall := false
	manifest := dto.ModelManifest{SchemaVersion: 1, Models: []dto.ModelManifestEntry{
		{ID: "allowed\"model", Visible: true, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI, constant.EndpointTypeOpenAIResponse}, Capabilities: &dto.ModelCapabilities{
			ReasoningEfforts: []string{}, InputModalities: []string{"text", "audio"}, ContextLimit: &contextLimit, OutputLimit: &outputLimit, ToolCall: &toolCall,
		}},
		{ID: "unknown", Visible: true, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}},
		{ID: "other-protocol", Visible: true, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeAnthropic}},
		{ID: "hidden", Visible: false, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}},
	}}
	pi, err := ExportModelClientConfig(manifest, "pi", "", "https://gateway.example.test/prefix/")
	require.NoError(t, err)
	assert.Equal(t, []string{"allowed\"model", "unknown"}, pi.Models)
	var parsed struct {
		Providers map[string]struct {
			BaseURL string           `json:"baseUrl"`
			APIKey  string           `json:"apiKey"`
			API     string           `json:"api"`
			Models  []map[string]any `json:"models"`
		} `json:"providers"`
	}
	require.NoError(t, common.Unmarshal([]byte(pi.Content), &parsed))
	provider := parsed.Providers["ren2hub"]
	assert.Equal(t, "https://gateway.example.test/prefix/v1", provider.BaseURL)
	assert.Equal(t, "${REN2HUB_API_KEY}", provider.APIKey)
	assert.Equal(t, "openai-completions", provider.API)
	assert.Equal(t, false, provider.Models[0]["reasoning"])
	assert.Equal(t, []any{"text"}, provider.Models[0]["input"])
	assert.NotContains(t, provider.Models[1], "reasoning")
	assert.NotContains(t, provider.Models[1], "input")
	assert.NotContains(t, provider.Models[1], "contextWindow")
	for _, item := range provider.Models {
		assert.NotContains(t, item, "cost")
	}
	assert.Contains(t, pi.Diagnostics, ModelClientConfigDiagnostic{Model: "allowed\"model", Code: "pi_input_modality_omitted"})
	codex, err := ExportModelClientConfig(manifest, "codex", "", "https://gateway.example.test/v1")
	require.NoError(t, err)
	var config map[string]any
	_, err = toml.Decode(codex.Content, &config)
	require.NoError(t, err)
	assert.Equal(t, "allowed\"model", config["model"])
	assert.Equal(t, contextLimit, config["model_context_window"])
	assert.NotContains(t, codex.Content, "other-protocol")
	assert.NotContains(t, codex.Content, "unknown")
	assert.Contains(t, codex.Content, `env_key = "REN2HUB_API_KEY"`)
	assert.Contains(t, codex.Content, `wire_api = "responses"`)
}

func TestModelClientConfigRejectsUnavailableModelsAndUnsafeAddresses(t *testing.T) {
	manifest := dto.ModelManifest{Models: []dto.ModelManifestEntry{{ID: "allowed", Visible: true, SupportedEndpointTypes: []constant.EndpointType{constant.EndpointTypeOpenAI}}}}
	for _, address := range []string{"", "//example.test", "javascript:alert(1)", "https://secret@example.test", "https://example.test?api_key=secret", "https://example.test/#fragment"} {
		_, err := ExportModelClientConfig(manifest, "pi", "", address)
		require.Error(t, err, address)
		assert.NotContains(t, err.Error(), "secret")
	}
	for _, format := range []string{"unsupported", "codex"} {
		_, err := ExportModelClientConfig(manifest, format, "", "https://example.test")
		require.Error(t, err)
	}
	_, err := ExportModelClientConfig(manifest, "pi", "forbidden", "https://example.test")
	require.Error(t, err)
}

func TestModelManifestRejectsDisabledExpiredAndDeletedTokens(t *testing.T) {
	db, userID, tokenID := setupModelManifestTest(t)
	input := ModelManifestRequest{UserID: userID, TokenID: tokenID}
	for _, update := range []map[string]any{
		{"status": common.TokenStatusDisabled},
		{"status": common.TokenStatusEnabled, "expired_time": common.GetTimestamp() - 1},
	} {
		require.NoError(t, db.Model(&model.Token{}).Where("id = ?", tokenID).Updates(update).Error)
		manifest, err := BuildModelManifest(context.Background(), input)
		require.ErrorIs(t, err, model.ErrTokenInvalid)
		assert.Empty(t, manifest.Models)
	}
	require.NoError(t, db.Delete(&model.Token{}, tokenID).Error)
	_, err := BuildModelManifest(context.Background(), input)
	require.Error(t, err)
}
