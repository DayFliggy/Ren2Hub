package middleware

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/pkg/requestfield"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"net/http"
)

// RejectConflictingRequestFields runs after authentication and before any
// request-specific routing, admission or billing. Cached bodies remain replayable.
func RejectConflictingRequestFields() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Body == nil || c.Request.Body == http.NoBody {
			c.Next()
			return
		}
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			abortWithOpenAiMessage(c, invalidRequestStatus(err), i18n.T(c, i18n.MsgDistributorInvalidRequest, map[string]any{"Error": err.Error()}))
			return
		}
		body, err := storage.Bytes()
		if err != nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "failed to read request body", types.ErrorCode("request_body_read_failed"))
			return
		}
		if field, conflict := requestfield.ConflictingField(c.GetHeader("Content-Type"), body); conflict {
			abortWithOpenAiMessage(c, http.StatusBadRequest, "duplicate request field: "+field, types.ErrorCode("duplicate_request_field"))
			return
		}
		c.Next()
	}
}
