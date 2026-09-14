package controller

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayAttemptDisconnectedWriterIsClientFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	info := &relaycommon.RelayInfo{UpstreamSent: true, StreamStatus: relaycommon.NewStreamStatus()}
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonClientGone, errors.New("client closed websocket"))
	apiErr := types.NewOpenAIError(errors.New("stream closed"), types.ErrorCodeBadResponse, http.StatusBadGateway)
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.NoError(t, c.Request.Context().Err(), "hijacked connections do not cancel the HTTP context")
	assert.Equal(t, service.AttemptOriginClientCancel, outcome.Origin)
	assert.Equal(t, service.AttemptRetryForbidden, outcome.RetryPermission)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.False(t, classification.MarkChannelModel)
	assert.False(t, classification.MarkKey)
	assert.False(t, classification.Failoverable)
}

type failedStreamWriter struct{ gin.ResponseWriter }

func (w *failedStreamWriter) WriteString(string) (int, error) { return 0, io.ErrClosedPipe }

func TestRelayAttemptLegacyCallbackWriteFailureDoesNotPenalizeProvider(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Writer = &failedStreamWriter{ResponseWriter: c.Writer}
	writeErr := helper.StringData(c, `{"type":"message_stop"}`)
	require.ErrorIs(t, writeErr, helper.ErrStreamClientWrite)
	info := &relaycommon.RelayInfo{UpstreamSent: true, StreamStatus: relaycommon.NewStreamStatus()}
	// Legacy scanners may have recorded handler_stop before the controller
	// classifies the retained client error; the first end reason is immutable.
	info.StreamStatus.SetEndReason(relaycommon.StreamEndReasonHandlerStop, writeErr)
	apiErr := types.NewOpenAIError(writeErr, types.ErrorCodeBadResponse, http.StatusBadGateway)
	outcome := relayAttemptOutcome(c, info, apiErr, time.Now(), false)
	assert.Equal(t, service.AttemptOriginClientCancel, outcome.Origin)
	classification := service.ClassifyAttemptOutcome(outcome)
	assert.False(t, classification.MarkChannelModel)
	assert.False(t, classification.MarkKey)
	assert.False(t, classification.Failoverable)
}
