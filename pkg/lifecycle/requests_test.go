package lifecycle

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestsSealRejectsAdmissionAndWaitsForCleanup(t *testing.T) {
	var requests Requests
	requestContext, finish, accepted := requests.Begin(context.Background())
	require.True(t, accepted)
	requests.Seal()
	_, _, accepted = requests.Begin(context.Background())
	require.False(t, accepted)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, requests.Stop(ctx), context.Canceled)
	require.ErrorIs(t, requestContext.Err(), context.Canceled)
	finish()
	finish()
	require.NoError(t, requests.Stop(ctx), "completed drain must remain observable after its deadline")
}

func TestRequestsIncludesHijackedHandlerAfterHTTPShutdown(t *testing.T) {
	var requests Requests
	hijacked := make(chan error, 1)
	finishHandler := make(chan struct{})
	handlerDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, done, accepted := requests.Begin(request.Context())
		if !accepted {
			hijacked <- fmt.Errorf("request rejected before shutdown")
			return
		}
		defer close(handlerDone)
		defer done()
		conn, _, err := writer.(http.Hijacker).Hijack()
		hijacked <- err
		if err != nil {
			return
		}
		defer conn.Close()
		<-finishHandler
	}))
	defer server.Close()
	client, err := net.Dial("tcp", server.Listener.Addr().String())
	require.NoError(t, err)
	defer client.Close()
	_, err = fmt.Fprint(client, "GET /realtime HTTP/1.1\r\nHost: test\r\n\r\n")
	require.NoError(t, err)
	require.NoError(t, <-hijacked)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, server.Config.Shutdown(ctx))
	canceled, stop := context.WithCancel(context.Background())
	stop()
	require.ErrorIs(t, requests.Stop(canceled), context.Canceled)
	close(finishHandler)
	require.NoError(t, requests.Stop(ctx))
	<-handlerDone
}
