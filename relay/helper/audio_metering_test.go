package helper

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestAudioMeteringRejectsFormatsWithoutActualUsage(t *testing.T) {
	token := billing.PriceVersion{Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "1"}}}
	info := &relaycommon.RelayInfo{RelayMode: relayconstant.RelayModeAudioSpeech, IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeOpenAI}}
	require.ErrorContains(t, validateAudioMetering(info, token), "missing_usage_contract")
	info.BillingAudioStreamFormat = "sse"
	require.NoError(t, validateAudioMetering(info, token))
	info.RelayMode = relayconstant.RelayModeAudioTranscription
	info.BillingAudioResponseFormat = "text"
	require.ErrorContains(t, validateAudioMetering(info, token), "missing_usage_contract")
	info.BillingAudioResponseFormat = "json"
	require.NoError(t, validateAudioMetering(info, token))
}

func TestRealtimeCannotEnableUnpricedProductsOrChangeFrozenTier(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "realtime", Mode: billing.ModeExpression, Expression: "p+c+ai+ao"})
	_, err := ModelPriceHelper(c, info, 1, nil)
	require.NoError(t, err)
	for _, body := range []string{
		`{"type":"session.update","session":{"service_tier":"priority"}}`,
		`{"type":"session.update","session":{"input_audio_transcription":{"model":"whisper-1"}}}`,
		`{"type":"response.create","response":{"tools":[{"type":"web_search"}]}}`,
		`{"type":"conversation.item.create","item":{"content":[{"type":"input_image","image_url":"data:image/png;base64,AA=="}]}}`,
	} {
		require.Error(t, ValidateRealtimeBillingEvent(info, []byte(body)), body)
	}
	require.NoError(t, ValidateRealtimeBillingEvent(info, []byte(`{"type":"session.update","session":{"instructions":"be brief"}}`)))
}
