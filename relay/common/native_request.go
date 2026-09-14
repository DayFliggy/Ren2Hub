package common

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// NativeRequestBody preserves extensions in an explicitly enabled passthrough
// body while still applying the selected model and channel instructions.
func NativeRequestBody(c *gin.Context, info *RelayInfo, request any) (io.Reader, io.Closer, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, nil, err
	}
	data, err := storage.Bytes()
	if err != nil {
		return nil, nil, err
	}
	var payload map[string]json.RawMessage
	if err := common.Unmarshal(data, &payload); err != nil {
		return nil, nil, err
	}
	if payload == nil {
		return nil, nil, fmt.Errorf("native request body must be a JSON object")
	}
	prepared, err := common.Marshal(request)
	if err != nil {
		return nil, nil, err
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(prepared, &fields); err != nil {
		return nil, nil, err
	}
	if _, gemini := request.(*dto.GeminiChatRequest); gemini {
		if value, exists := payload["system_instruction"]; exists {
			payload["systemInstruction"] = value
			delete(payload, "system_instruction")
		}
	}
	if value, exists := fields["model"]; exists {
		// DTO decoding accepts case variants. Do not leave a second model or
		// instruction field that could override the selected value upstream.
		for original := range payload {
			if strings.EqualFold(original, "model") {
				delete(payload, original)
			}
		}
		payload["model"] = value
	}
	if err := applyChannelSystemPromptJSON(c, info, request, payload); err != nil {
		return nil, nil, err
	}
	data, err = common.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	if len(info.ParamOverride) > 0 {
		data, err = ApplyParamOverrideWithRelayInfo(data, info)
		if err != nil {
			if fixed, ok := AsParamOverrideReturnError(err); ok {
				return nil, nil, NewAPIErrorFromParamOverride(fixed)
			}
			return nil, nil, types.NewError(err, types.ErrorCodeChannelParamOverrideInvalid, types.ErrOptionWithSkipRetry())
		}
	}
	return NewOutboundJSONBody(data)
}
