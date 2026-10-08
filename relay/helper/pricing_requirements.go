package helper

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
)

// PricingRequirementsFromBody inspects the final wire request. Declarations
// require a price contract but do not become completed tool-call quantities.
func PricingRequirementsFromBody(info *relaycommon.RelayInfo, body []byte) (billing.PricingRequirements, error) {
	required := billing.PricingRequirements{}
	var payload map[string]any
	if len(body) > 0 {
		if err := common.DecodeJsonNumbers(bytes.NewReader(body), &payload); err != nil {
			return required, err
		}
		if payload == nil {
			return required, fmt.Errorf("request must be a JSON object")
		}
	}
	if raw, present := payload["service_tier"]; present {
		tier, ok := raw.(string)
		if !ok || strings.TrimSpace(tier) == "" {
			return required, fmt.Errorf("invalid service_tier")
		}
		required.ServiceTier = tier
	}
	dimensions := map[string]bool{}
	if options, present := payload["web_search_options"]; present && options != nil {
		if _, ok := options.(map[string]any); !ok {
			return required, fmt.Errorf("invalid web_search_options")
		}
		dimensions["tool:"+dto.BuildInToolWebSearchPreview] = true
	}
	if strings.HasSuffix(info.BillingModelName(), "search-preview") {
		dimensions["tool:"+dto.BuildInToolWebSearchPreview] = true
	}
	if info.GetFinalRequestRelayFormat() == types.RelayFormatOpenAIRealtime {
		dimensions["ai"], dimensions["ao"] = true, true
	}
	tools, _ := payload["tools"].([]any)
	claudeWire := info.GetFinalRequestRelayFormat() == types.RelayFormatClaude
	for _, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			return required, fmt.Errorf("invalid tool declaration")
		}
		kind, _ := tool["type"].(string)
		switch {
		case strings.HasPrefix(kind, "web_search_preview"):
			dimensions["tool:"+dto.BuildInToolWebSearchPreview] = true
		case strings.HasPrefix(kind, "web_search"):
			dimensions["tool:"+dto.BuildInToolWebSearch] = true
		case kind == "file_search", kind == "image_generation":
			dimensions["tool:"+kind] = true
		case kind == "code_interpreter", kind == "computer_use_preview", kind == "computer":
			return required, &billing.PricingError{Code: "missing_usage_contract", Dimension: "tool:" + kind}
		}
		// Anthropic reports these server tools in usage.server_tool_use, and the
		// Claude handler bills each count under the tool's own key.
		if claudeWire {
			for _, name := range []string{"web_fetch", "code_execution", "tool_search"} {
				if strings.HasPrefix(kind, name) {
					dimensions["tool:"+name] = true
				}
			}
		}
		// The channel bills this hosted tool under its own price keys as well.
		for prefix, keys := range info.VendorToolPriceKeys {
			if strings.HasPrefix(kind, prefix) {
				for _, key := range keys {
					dimensions["tool:"+key] = true
				}
			}
		}
		for _, name := range []string{"googleSearch", "google_search", "googleSearchRetrieval", "google_search_retrieval"} {
			if _, present := tool[name]; present {
				dimensions["tool:"+dto.BuildInToolGoogleSearch] = true
			}
		}
		for _, name := range []string{"googleMaps", "google_maps"} {
			if _, present := tool[name]; present {
				return required, &billing.PricingError{Code: "missing_usage_contract", Dimension: "tool:google_maps"}
			}
		}
	}
	for _, name := range []string{"modalities", "responseModalities"} {
		modalities, _ := payload[name].([]any)
		for _, raw := range modalities {
			if value, ok := raw.(string); ok && strings.EqualFold(value, "audio") {
				dimensions["ao"] = true
			}
		}
	}
	if config, ok := payload["generationConfig"].(map[string]any); ok {
		for _, name := range []string{"responseModalities", "response_modalities"} {
			if values, ok := config[name].([]any); ok {
				for _, value := range values {
					if label, ok := value.(string); ok && strings.EqualFold(label, "audio") {
						dimensions["ao"] = true
					}
				}
			}
		}
	}
	var visitContent func(any)
	visitContent = func(value any) {
		switch node := value.(type) {
		case []any:
			for _, child := range node {
				visitContent(child)
			}
		case map[string]any:
			switch node["type"] {
			case "input_audio", "audio":
				dimensions["ai"] = true
			}
			for _, key := range []string{"content", "parts"} {
				visitContent(node[key])
			}
			for _, key := range []string{"inlineData", "inline_data", "fileData", "file_data"} {
				if media, ok := node[key].(map[string]any); ok {
					mime, _ := media["mimeType"].(string)
					if mime == "" {
						mime, _ = media["mime_type"].(string)
					}
					if strings.HasPrefix(mime, "audio/") {
						dimensions["ai"] = true
					}
				}
			}
		}
	}
	for _, key := range []string{"messages", "input", "contents"} {
		visitContent(payload[key])
	}
	for name := range dimensions {
		required.Dimensions = append(required.Dimensions, name)
	}
	sort.Strings(required.Dimensions)
	return required, nil
}

func BillingPriceAPIError(err error) *types.NewAPIError {
	billing.RecordMissingPrice()
	code := types.ErrorCodeModelPriceError
	var priced interface{ ErrorCode() string }
	if errors.As(err, &priced) {
		code = types.ErrorCode(priced.ErrorCode())
	}
	return types.NewErrorWithStatusCode(err, code, 503, types.ErrOptionWithSkipRetry())
}

func BillingRequestAPIError(err error) *types.NewAPIError {
	if _, ok := err.(interface{ ErrorCode() string }); ok {
		return BillingPriceAPIError(err)
	}
	return types.NewErrorWithStatusCode(err, types.ErrorCodeInvalidRequest, 400, types.ErrOptionWithSkipRetry())
}

// A Realtime connection keeps its model identity and cannot enable a product
// for which the protocol provides no independent usage contract.
func ValidateRealtimeBillingEvent(info *relaycommon.RelayInfo, body []byte) error {
	quote := info.PriceData.Quote
	if quote == nil {
		return &billing.PricingError{Code: "missing_price_dimension", Dimension: "quote"}
	}
	var event map[string]any
	if err := common.DecodeJsonNumbers(bytes.NewReader(body), &event); err != nil {
		return err
	}
	var payload map[string]any
	switch event["type"] {
	case "session.update":
		payload, _ = event["session"].(map[string]any)
	case "response.create":
		payload, _ = event["response"].(map[string]any)
	}
	if payload == nil {
		payload = map[string]any{}
	}
	if value, present := payload["input_audio_transcription"]; present && value != nil {
		return &billing.PricingError{Code: "missing_usage_contract", Dimension: "input_audio_transcription"}
	}
	if audio, ok := payload["audio"].(map[string]any); ok {
		if input, ok := audio["input"].(map[string]any); ok && input["transcription"] != nil {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: "input_audio_transcription"}
		}
	}
	if name, ok := payload["model"].(string); ok && name != "" && name != info.UpstreamModelName {
		return fmt.Errorf("realtime model cannot change within a frozen quote")
	}
	wire, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	required, err := PricingRequirementsFromBody(info, wire)
	if err != nil {
		return err
	}
	if item, ok := event["item"].(map[string]any); ok {
		if content, ok := item["content"].([]any); ok {
			for _, raw := range content {
				if part, ok := raw.(map[string]any); ok && part["type"] == "input_image" {
					required.Dimensions = append(required.Dimensions, "img")
				}
			}
		}
	}
	for _, dimension := range required.Dimensions {
		if strings.HasPrefix(dimension, "tool:") {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: dimension}
		}
	}
	if rawTools, present := payload["tools"]; present {
		tools, ok := rawTools.([]any)
		if !ok {
			return fmt.Errorf("invalid realtime tools")
		}
		for _, raw := range tools {
			if tool, ok := raw.(map[string]any); ok {
				kind, _ := tool["type"].(string)
				if kind != "function" {
					return &billing.PricingError{Code: "missing_usage_contract", Dimension: "tool:" + kind}
				}
				name, _ := tool["name"].(string)
				for _, price := range quote.PriceVersion.Prices {
					if price.Quantity == "tool:"+name {
						return &billing.PricingError{Code: "missing_usage_contract", Dimension: price.Quantity}
					}
				}
			}
		}
	}
	return billing.ValidatePricingRequirements(quote.PriceVersion, required)
}
