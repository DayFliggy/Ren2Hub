package helper

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
)

// FinalAudioPriceMeta freezes the final provider input. Character counts are
// billable only when the published plan explicitly selects that counting method.
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
	if raw := payload["stream_format"]; len(raw) > 0 {
		if err := common.Unmarshal(raw, &info.BillingAudioStreamFormat); err != nil {
			return nil, 0, fmt.Errorf("invalid stream_format")
		}
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
	info.BillingCharacterCount = utf8.RuneCountInString(input)
	tokens := info.BillingCharacterCount
	if meta.TokenType != types.TokenTypeTextNumber {
		tokens = service.CountTextToken(input, info.BillingModelName())
	}
	return meta, tokens, nil
}

// FreezeTranscriptionPriceMeta reads only billing-relevant scalar fields from
// the final multipart request, never audio bytes or transcripts.
func FreezeTranscriptionPriceMeta(info *relaycommon.RelayInfo, contentType string, body io.Reader) error {
	_, parameters, err := mime.ParseMediaType(contentType)
	if err != nil || parameters["boundary"] == "" || body == nil {
		return fmt.Errorf("invalid transcription multipart request")
	}
	fields := map[string]any{}
	reader := multipart.NewReader(body, parameters["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := part.FormName()
		if part.FileName() == "" && (name == "response_format" || name == "service_tier") {
			if _, exists := fields[name]; exists {
				return fmt.Errorf("duplicate transcription field")
			}
			value, err := io.ReadAll(io.LimitReader(part, 129))
			if err != nil || len(value) > 128 {
				return fmt.Errorf("invalid transcription field")
			}
			fields[name] = string(value)
		}
		if err := part.Close(); err != nil {
			return err
		}
	}
	info.BillingAudioResponseFormat, _ = fields["response_format"].(string)
	data, err := common.Marshal(fields)
	if err != nil {
		return err
	}
	return FreezeOutboundBillingAttributes(info, data)
}
