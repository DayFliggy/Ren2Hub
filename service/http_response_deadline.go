package service

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// A streaming request can receive JSON or an error response instead. Those
// bodies never enter the stream scanner, so they need a completion deadline.
type streamResponseDeadlineTransport struct {
	base    http.RoundTripper
	timeout time.Duration
}

func (t *streamResponseDeadlineTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	started := time.Now()
	ctx, cancel := context.WithCancelCause(request.Context())
	// Transport.ResponseHeaderTimeout starts after the upload finishes. This
	// deadline also bounds an upstream that stops reading a streaming request.
	headerTimer := time.AfterFunc(common.RelayPhaseTimeout(common.RelayResponseHeaderTimeout), func() { cancel(context.DeadlineExceeded) })
	response, err := t.base.RoundTrip(request.Clone(ctx))
	headerTimer.Stop()
	if err != nil {
		if cause := context.Cause(ctx); cause != nil {
			err = cause
		}
		cancel(err)
		return response, err
	}
	mediaType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	streaming := false
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		switch strings.ToLower(mediaType) {
		case "text/event-stream", "application/x-ndjson", "application/ndjson", "application/jsonl", "application/vnd.amazon.eventstream":
			streaming = true
		}
	}
	body := &deadlineResponseBody{ReadCloser: response.Body, ctx: ctx, cancel: cancel}
	if !streaming {
		body.timer = time.AfterFunc(t.timeout-time.Since(started), func() { cancel(context.DeadlineExceeded) })
	}
	response.Body = body
	return response, nil
}

func (t *streamResponseDeadlineTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type deadlineResponseBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelCauseFunc
	timer  *time.Timer
}

func (b *deadlineResponseBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if err != nil {
		if cause := context.Cause(b.ctx); cause != nil {
			return n, cause
		}
		if b.timer != nil {
			b.timer.Stop()
		}
	}
	return n, err
}

func (b *deadlineResponseBody) Close() error {
	if b.timer != nil {
		b.timer.Stop()
	}
	b.cancel(context.Canceled)
	return b.ReadCloser.Close()
}
