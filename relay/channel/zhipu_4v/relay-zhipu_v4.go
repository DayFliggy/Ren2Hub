package zhipu_4v

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func requestOpenAI2Zhipu(request dto.GeneralOpenAIRequest) (*dto.GeneralOpenAIRequest, error) {
	// Only inline image encoding differs. Preserve tool history and explicit
	// false/zero controls when normalizing the provider's request envelope.
	request.Messages = append([]dto.Message(nil), request.Messages...)
	for index, message := range request.Messages {
		if message.Content == nil || message.IsStringContent() {
			continue
		}
		encoded, err := common.Marshal(message.Content)
		if err != nil {
			return nil, err
		}
		var parts []json.RawMessage
		if err := common.Unmarshal(encoded, &parts); err != nil {
			return nil, err
		}
		for i, part := range parts {
			if gjson.GetBytes(part, "type").String() != dto.ContentTypeImageURL {
				continue
			}
			value := gjson.GetBytes(part, "image_url.url").String()
			if strings.HasPrefix(value, "data:image/") {
				if _, data, ok := strings.Cut(value, ","); ok {
					parts[i], err = sjson.SetBytes(part, "image_url.url", data)
					if err != nil {
						return nil, err
					}
				}
			}
		}
		request.Messages[index].Content = parts
	}
	if request.MaxTokens != nil || request.MaxCompletionTokens != nil {
		maxTokens := request.GetMaxTokens()
		request.MaxTokens, request.MaxCompletionTokens = &maxTokens, nil
	}
	return &request, nil
}
