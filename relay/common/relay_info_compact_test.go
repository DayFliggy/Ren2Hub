package common

import (
	"net/http"
	"net/http/httptest"
	"testing"

	rootcommon "github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCompactRelayInfoPreservesRequestedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct{ name, model string }{
		{"ordinary", "gpt-5"},
		{"literal suffix", "gpt-5-openai-compact"},
		{"non gpt", "claude-3-5-sonnet"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/responses/compact", nil)
			rootcommon.SetContextKey(ctx, constant.ContextKeyOriginalModel, test.model)

			info := GenRelayInfoResponsesCompaction(ctx, &dto.OpenAIResponsesCompactionRequest{})
			require.Equal(t, test.model, info.OriginModelName)
			require.Equal(t, test.model, info.BillingModelName())
		})
	}
}
