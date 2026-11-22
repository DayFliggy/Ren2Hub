package perfmetrics

import (
	"context"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestOutcomeClassificationUsesOriginalTypeAfterErrorMasking(t *testing.T) {
	err := types.WithOpenAIError(types.OpenAIError{Message: "failure", Type: "invalid_request_error"}, 500)
	err.BindSensitiveContext(nil, "invalid_request_error")
	assert.Equal(t, "***", err.ToOpenAIError().Type)
	assert.Equal(t, OutcomeIgnored, ClassifyRelayOutcome(context.Background(), &relaycommon.RelayInfo{}, err))
}
