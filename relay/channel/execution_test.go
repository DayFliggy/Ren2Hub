package channel

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/pkg/execution"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestHTTPExecutionBudgetCountsOnlyCallsAtTransmissionBoundary(t *testing.T) {
	service.InitHttpClient()
	var received atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusBadGateway)
	}))
	defer upstream.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	state := RequestExecution(c, execution.Generation)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 2}}
	_, err := doRequest(c, &http.Request{}, info)
	require.Error(t, err)
	require.Zero(t, state.CallCount())
	invalidHeader, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewBufferString(`{}`))
	require.NoError(t, err)
	invalidHeader.Header.Set("X-Test", "invalid\r\nvalue")
	_, err = doRequest(c, invalidHeader, info)
	require.Error(t, err)
	require.Zero(t, state.CallCount())
	for index := 0; index < 3; index++ {
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstream.URL, bytes.NewBufferString(`{"model":"m"}`))
		require.NoError(t, err)
		response, err := doRequest(c, req, info)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
	}
	req, err := http.NewRequest(http.MethodPost, upstream.URL, bytes.NewBufferString(`{}`))
	require.NoError(t, err)
	_, err = doRequest(c, req, info)
	require.ErrorIs(t, err, execution.ErrCallBudget)
	require.EqualValues(t, 3, received.Load())
	require.Len(t, state.Snapshot().Candidates, 1)
	require.Len(t, state.Snapshot().Calls, 3)
}

func TestCandidateCheckRejectsUninitializedMetadataWithoutPanic(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	for _, info := range []*relaycommon.RelayInfo{nil, {}} {
		_, err := CheckExecutionCandidate(c, info)
		require.ErrorContains(t, err, "initialized channel metadata")
	}
	_, err := CheckExecutionCandidate(nil, &relaycommon.RelayInfo{})
	require.Error(t, err)
	require.Zero(t, RequestExecution(c, execution.Submission).CallCount())
}
