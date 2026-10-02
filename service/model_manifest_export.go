package service

import (
	"bytes"
	"errors"
	"net/url"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
)

type ModelClientConfig struct {
	Format      string                        `json:"format"`
	Filename    string                        `json:"filename"`
	MediaType   string                        `json:"media_type"`
	Content     string                        `json:"content"`
	Models      []string                      `json:"models"`
	Diagnostics []ModelClientConfigDiagnostic `json:"diagnostics"`
}

type ModelClientConfigDiagnostic struct {
	Model string `json:"model,omitempty"`
	Code  string `json:"code"`
}

// ExportModelClientConfig receives only the permission-filtered projection.
// Neither token secrets nor the request Host participate in client configuration.
func ExportModelClientConfig(manifest dto.ModelManifest, format, selectedModel, serverAddress string) (ModelClientConfig, error) {
	result := ModelClientConfig{Format: format, Models: []string{}, Diagnostics: []ModelClientConfigDiagnostic{}}
	endpoint := constant.EndpointTypeOpenAI
	switch format {
	case "codex":
		endpoint = constant.EndpointTypeOpenAIResponse
		result.Filename, result.MediaType = "ren2hub.config.toml", "application/toml"
	case "pi":
		result.Filename, result.MediaType = "ren2hub.models.json", "application/json"
	default:
		return result, errors.New("unsupported client configuration format")
	}
	address, err := url.Parse(strings.TrimSpace(serverAddress))
	if err != nil || address.Hostname() == "" || (address.Scheme != "http" && address.Scheme != "https") || address.User != nil || address.RawQuery != "" || address.Fragment != "" {
		return result, errors.New("a trusted server address must be configured before exporting client configuration")
	}
	baseURL := strings.TrimRight(address.String(), "/")
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	entries := make([]dto.ModelManifestEntry, 0, len(manifest.Models))
	for _, entry := range manifest.Models {
		if !entry.Visible || !slices.Contains(entry.SupportedEndpointTypes, endpoint) {
			continue
		}
		result.Models = append(result.Models, entry.ID)
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return result, errors.New("this token has no models for the selected client protocol")
	}
	if selectedModel == "" {
		selectedModel = entries[0].ID
	}
	if !slices.Contains(result.Models, selectedModel) {
		return result, errors.New("the selected model is not available to this token and client protocol")
	}
	// Frozen/expression/channel-dependent prices do not have one portable client
	// price. Omit all cost fields rather than inventing free usage or a quote.
	result.Diagnostics = append(result.Diagnostics, ModelClientConfigDiagnostic{Code: "prices_omitted"})
	if format == "codex" {
		config := map[string]any{
			"model":          selectedModel,
			"model_provider": "ren2hub",
			"model_providers": map[string]any{"ren2hub": map[string]any{
				"name": "Ren2Hub", "base_url": baseURL, "env_key": "REN2HUB_API_KEY", "wire_api": "responses",
			}},
		}
		for _, entry := range entries {
			if entry.ID != selectedModel {
				continue
			}
			if entry.Capabilities != nil && entry.Capabilities.ContextLimit != nil {
				config["model_context_window"] = *entry.Capabilities.ContextLimit
			}
			if entry.Capabilities != nil {
				result.Diagnostics = append(result.Diagnostics, ModelClientConfigDiagnostic{Model: entry.ID, Code: "codex_capabilities_partial"})
			}
		}
		var encoded bytes.Buffer
		if err := toml.NewEncoder(&encoded).Encode(config); err != nil {
			return result, err
		}
		result.Content = encoded.String()
		return result, nil
	}
	models := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item := map[string]any{"id": entry.ID, "name": entry.ID}
		card := entry.Capabilities
		if card != nil {
			if card.ContextLimit != nil {
				item["contextWindow"] = *card.ContextLimit
			}
			if card.OutputLimit != nil {
				item["maxTokens"] = *card.OutputLimit
			}
			if card.ReasoningEfforts != nil {
				item["reasoning"] = len(card.ReasoningEfforts) > 0
			}
			if card.InputModalities != nil {
				inputs := make([]string, 0, len(card.InputModalities))
				for _, input := range card.InputModalities {
					if input == "text" || input == "image" {
						inputs = append(inputs, input)
					} else {
						result.Diagnostics = append(result.Diagnostics, ModelClientConfigDiagnostic{Model: entry.ID, Code: "pi_input_modality_omitted"})
					}
				}
				if len(inputs) > 0 {
					item["input"] = inputs
				} else {
					result.Diagnostics = append(result.Diagnostics, ModelClientConfigDiagnostic{Model: entry.ID, Code: "pi_input_unknown"})
				}
			}
			if card.ToolCall != nil || card.OutputModalities != nil {
				result.Diagnostics = append(result.Diagnostics, ModelClientConfigDiagnostic{Model: entry.ID, Code: "pi_capabilities_partial"})
			}
		}
		models = append(models, item)
	}
	encoded, err := common.Marshal(map[string]any{"providers": map[string]any{"ren2hub": map[string]any{
		"baseUrl": baseURL, "apiKey": "${REN2HUB_API_KEY}", "api": "openai-completions", "models": models,
	}}})
	if err != nil {
		return result, err
	}
	result.Content = string(encoded)
	return result, nil
}
