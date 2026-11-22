package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCodexResponseHeadersRespectConnectionTokens(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	copyCodexSSEHeaders(c, &http.Response{Header: http.Header{"Connection": {"X-Codex-Turn-State"}, "X-Codex-Turn-State": {"private-hop"}, "X-Reasoning-Included": {"true"}}})
	assert.Empty(t, w.Header().Get("X-Codex-Turn-State"))
	assert.Equal(t, "true", w.Header().Get("X-Reasoning-Included"))
}

func TestStreamErrorClassificationKeepsOriginalTypeAfterMasking(t *testing.T) {
	status := relaycommon.NewStreamStatus()
	result := newStreamResult(status, nil)
	err := types.WithOpenAIError(types.OpenAIError{Message: "upstream rejected", Type: "invalid_request_error"}, 500)
	err.BindSensitiveContext(nil, "invalid_request_error")
	result.Error(err)
	assert.Equal(t, "invalid_request_error", status.OutcomeSnapshot().ErrorType)
	assert.Equal(t, "***", err.ToOpenAIError().Type)
}
