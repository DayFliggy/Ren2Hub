package relay

import (
	"fmt"
	"github.com/QuantumNous/new-api/pkg/requestfield"
	"github.com/QuantumNous/new-api/relaykit/types"
	"net/http"
)

func responsesWSFieldConflict(message []byte) *types.NewAPIError {
	if field, conflict := requestfield.ConflictingField("application/json", message); conflict {
		return types.NewErrorWithStatusCode(fmt.Errorf("duplicate request field: %s", field), types.ErrorCode("duplicate_request_field"), http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}
	return nil
}
