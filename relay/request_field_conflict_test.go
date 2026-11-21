package relay

import (
	"errors"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestResponsesWSRejectsConflictingFieldsBeforeRunner(t *testing.T) {
	client, server, cleanup := newTestWebSocketPair(t)
	defer cleanup()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/v1/responses", nil)
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		ResponsesWebSocketHelper(c, server, func(_ *http.Request, _ string, _ func(*gin.Context) *types.NewAPIError) *types.NewAPIError {
			calls.Add(1)
			return types.NewErrorWithStatusCode(errors.New("runner reached"), types.ErrorCodeInvalidRequest, 400)
		})
	}()
	defer func() { client.Close(); <-done }()
	require.NoError(t, client.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.create","model":"cheap","Model":"expensive"}`)))
	client.SetReadDeadline(time.Now().Add(time.Second * 3))
	_, body, err := client.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(body), "duplicate_request_field")
	require.Zero(t, calls.Load())
}
