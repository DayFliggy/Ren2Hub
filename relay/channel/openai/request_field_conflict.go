package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/requestfield"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// Reject before decoding, counting tokens or forwarding to keep every consumer
// on the same unambiguous fields. The session can accept the next client event.
func rejectRealtimeFieldConflict(c *gin.Context, message []byte, writeClient func([]byte) error) (bool, error) {
	field, conflict := requestfield.ConflictingField("application/json", message)
	if !conflict {
		return false, nil
	}
	event := dto.RealtimeEvent{
		Type: "error", EventId: helper.GetLocalRealtimeID(c),
		Error: &types.OpenAIError{Type: "invalid_request_error", Code: "duplicate_request_field", Message: "Duplicate request field: " + field, Param: field},
	}
	encoded, err := common.Marshal(event)
	if err != nil {
		return true, err
	}
	return true, writeClient(encoded)
}
