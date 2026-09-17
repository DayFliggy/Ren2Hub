package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayResponseHeaderDeadlineCancelsUpstream(t *testing.T) {
	previous := common.RelayResponseHeaderTimeout
	common.RelayResponseHeaderTimeout = 1
	t.Cleanup(func() { common.RelayResponseHeaderTimeout = previous })
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	client := newRelayHTTPClient(newRelayHTTPTransport())
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
	require.NoError(t, err)
	_, err = client.Do(request)
	require.Error(t, err)
	var timeout interface{ Timeout() bool }
	require.ErrorAs(t, err, &timeout)
	assert.True(t, timeout.Timeout())
	assert.NoError(t, ctx.Err(), "the gateway header deadline must fire before the client request deadline")
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("upstream request was not cancelled by the header timeout")
	}
}

func TestRelayNonStreamDeadlineCancelsUnfinishedBody(t *testing.T) {
	previous := common.RelayNonStreamTimeout
	common.RelayNonStreamTimeout = 1
	t.Cleanup(func() { common.RelayNonStreamTimeout = previous })
	for _, tc := range []struct {
		name      string
		streaming bool
		status    int
	}{
		{"json request", false, http.StatusOK},
		{"stream request with JSON response", true, http.StatusOK},
		{"stream request with error response", true, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cancelled := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"partial":`)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(cancelled)
			}))
			defer server.Close()
			shared := newRelayHTTPClient(newRelayHTTPTransport())
			defer shared.CloseIdleConnections()
			client := RelayRequestHTTPClient(shared, tc.streaming)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			require.NoError(t, err)
			response, err := client.Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.Error(t, err)
			assert.Contains(t, string(body), "partial")
			assert.True(t, errors.Is(err, context.DeadlineExceeded))
			assert.NoError(t, ctx.Err())
			select {
			case <-cancelled:
			case <-ctx.Done():
				t.Fatal("unfinished upstream body was not cancelled")
			}
		})
	}
}

func TestRelayRequestDeadlinePreservesStreamAndStricterCaps(t *testing.T) {
	previous := common.RelayTimeout
	t.Cleanup(func() { common.RelayTimeout = previous })
	for _, tc := range []struct {
		name      string
		cap       time.Duration
		streaming bool
		want      time.Duration
	}{
		{"nonstream default", 0, false, 300 * time.Second},
		{"stream no total cap", 0, true, 0},
		{"nonstream stricter cap", time.Second, false, time.Second},
		{"stream explicit cap", time.Second, true, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			common.RelayTimeout = int(tc.cap / time.Second)
			shared := &http.Client{Timeout: tc.cap}
			client := RelayRequestHTTPClient(shared, tc.streaming)
			assert.Equal(t, tc.want, client.Timeout)
			assert.Equal(t, tc.cap, shared.Timeout, "shared clients must not be mutated")
		})
	}
}

type deadlineTestTransport func(*http.Request) (*http.Response, error)

func (f deadlineTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStreamingResponseDoesNotInheritNonStreamDeadline(t *testing.T) {
	for _, contentType := range []string{"text/event-stream; charset=utf-8", "application/x-ndjson", "application/vnd.amazon.eventstream"} {
		t.Run(contentType, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var upstreamContext context.Context
				base := deadlineTestTransport(func(request *http.Request) (*http.Response, error) {
					upstreamContext = request.Context()
					reader, writer := io.Pipe()
					go func() {
						defer writer.Close()
						time.Sleep(2 * time.Second)
						_, _ = io.WriteString(writer, "complete")
					}()
					return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: reader}, nil
				})
				client := &http.Client{Transport: &streamResponseDeadlineTransport{base: base, timeout: time.Second}}
				response, err := client.Get("https://upstream.test")
				require.NoError(t, err)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Equal(t, "complete", string(body))
				assert.NoError(t, upstreamContext.Err())
				require.NoError(t, response.Body.Close())
				assert.ErrorIs(t, upstreamContext.Err(), context.Canceled)
			})
		})
	}
}

func TestStreamingRequestUploadIsBoundedBeforeResponseHeaders(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var upstreamContext context.Context
		base := deadlineTestTransport(func(request *http.Request) (*http.Response, error) {
			upstreamContext = request.Context()
			<-request.Context().Done()
			return nil, request.Context().Err()
		})
		client := &http.Client{Transport: &streamResponseDeadlineTransport{base: base, timeout: time.Second}}
		_, err := client.Get("https://upstream.test")
		require.ErrorIs(t, err, context.DeadlineExceeded)
		require.ErrorIs(t, context.Cause(upstreamContext), context.DeadlineExceeded)
	})
}
