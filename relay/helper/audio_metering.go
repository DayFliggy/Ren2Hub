package helper

import (
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
)

func validateAudioMetering(info *relaycommon.RelayInfo, version billing.PriceVersion) error {
	if !version.NeedsTokenUsage() {
		return nil
	}
	switch info.RelayMode {
	case relayconstant.RelayModeAudioTranscription, relayconstant.RelayModeAudioTranslation:
		switch info.BillingAudioResponseFormat {
		case "", "json", "verbose_json", "diarized_json":
		default:
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: "transcription_usage"}
		}
	case relayconstant.RelayModeAudioSpeech:
		// A binary body has no provider token counters. Fixed request pricing
		// needs only successful delivery; token pricing needs actual usage.
		if info.ApiType != constant.APITypeOpenAI || info.BillingAudioStreamFormat != "sse" {
			return &billing.PricingError{Code: "missing_usage_contract", Dimension: "audio_tokens"}
		}
	}
	return nil
}
