package helper

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

// FinalAudioPriceMeta follows the existing TTS character/token pricing rule
// using the actual provider input. It does not bound audio output for TPM.
func FinalAudioPriceMeta(info *relaycommon.RelayInfo, body io.Reader) (*types.TokenCountMeta, int, error) {
	if body == nil {
		return nil, 0, errors.New("final speech request is unavailable")
	}
	var payload map[string]json.RawMessage
	if err := common.DecodeJson(body, &payload); err != nil || payload == nil {
		return nil, 0, errors.New("invalid final speech request")
	}
	field := "input"
	switch info.ApiType {
	case constant.APITypeMiniMax:
		field = "text"
	case constant.APITypeVolcEngine:
		var request map[string]json.RawMessage
		if err := common.Unmarshal(payload["request"], &request); err != nil || request == nil {
			return nil, 0, errors.New("invalid final speech request parameters")
		}
		payload, field = request, "text"
	}
	var input string
	if common.Unmarshal(payload[field], &input) != nil || strings.TrimSpace(input) == "" {
		return nil, 0, errors.New("final speech input must be nonempty text")
	}
	request := &dto.AudioRequest{Model: info.BillingModelName(), Input: input}
	pricingPayload, err := common.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	if err := FreezeOutboundBillingAttributes(info, pricingPayload); err != nil {
		return nil, 0, err
	}
	meta := request.GetTokenCountMeta()
	tokens := utf8.RuneCountInString(input)
	if meta.TokenType != types.TokenTypeTextNumber {
		tokens = service.CountTextToken(input, info.BillingModelName())
	}
	return meta, tokens, nil
}
