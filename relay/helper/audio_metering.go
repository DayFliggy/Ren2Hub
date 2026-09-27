package helper

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

func validateAudioMetering(info *relaycommon.RelayInfo, version billing.PriceVersion) error {
	kind := ""
	switch info.RelayMode {
	case relayconstant.RelayModeAudioSpeech:
		kind = "speech"
	case relayconstant.RelayModeAudioTranscription, relayconstant.RelayModeAudioTranslation:
		kind = "transcription"
	}
	if kind == "transcription" && version.Mode != billing.ModeFree && (version.NeedsTokenUsage() || version.Metering != nil) {
		switch info.BillingAudioResponseFormat {
		case "", "json", "verbose_json", "diarized_json":
		default:
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: "transcription_usage"}
		}
	}
	contract := version.Metering
	if contract != nil {
		if kind != contract.RequestKind {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: contract.Quantity}
		}
		if contract.Source == "request_fixed" {
			if info.BillingCharacterCount <= 0 {
				return &billing.PricingError{Code: "missing_usage_contract", Dimension: "characters"}
			}
			return nil
		}
		if kind == "speech" && info.ApiType != constant.APITypeMiniMax || kind == "transcription" && info.ApiType != constant.APITypeOpenAI {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: contract.Quantity}
		}
		return nil
	}
	if kind == "" || version.Mode == billing.ModeFree {
		return nil
	}
	for _, price := range version.Prices {
		if price.Quantity == "characters" || price.Quantity == "seconds" {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: price.Quantity}
		}
	}
	// Token-billed audio requires a protocol that reports actual token usage.
	// A binary speech body alone never supplies that fact.
	if kind == "speech" && (info.ApiType != constant.APITypeOpenAI || info.BillingAudioStreamFormat != "sse") {
		if version.NeedsTokenUsage() {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: "audio_tokens"}
		}
	}
	return nil
}
