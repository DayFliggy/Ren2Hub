//go:build known_upstream_restored

package helper

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"io"
	"strings"
	"testing"
)

type restoredErrorWithCompletionBody struct{ data *strings.Reader }

func (b *restoredErrorWithCompletionBody) Read(p []byte) (int, error) {
	n, _ := b.data.Read(p)
	return n, io.ErrUnexpectedEOF
}
func (*restoredErrorWithCompletionBody) Close() error { return nil }
func TestRestoredProbeStreamReadErrorAtCompletion(t *testing.T) {
	c, resp, info := setupStreamTest(t, strings.NewReader(""))
	resp.Body = &restoredErrorWithCompletionBody{strings.NewReader(strings.Repeat("data: complete\n\n", 20))}
	info.DisablePing = true
	StreamScannerHandler(c, resp, info, func(_ string, sr *StreamResult) { info.StreamStatus.MarkCompleted(); sr.Done() })
	assert.Equal(t, relaycommon.StreamEndReasonScannerErr, info.StreamStatus.EndReason)
	assert.ErrorIs(t, info.StreamStatus.EndError, io.ErrUnexpectedEOF)
	assert.False(t, info.StreamStatus.IsNormalEnd())
}
