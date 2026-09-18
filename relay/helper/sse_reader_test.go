package helper

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBufferedSSEReaderPreservesCompleteEvents(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		body := "\ufeff" + strings.Join([]string{": ping", "event: response.completed", "data: {", `data: "response":{"status":"completed"}}`}, newline)
		var got []sseEvent
		err := ReadSSEEvents(strings.NewReader(body), func(data, name string) (bool, error) {
			got = append(got, sseEvent{data: data, name: name})
			return false, nil
		})
		require.NoError(t, err)
		assert.Equal(t, []sseEvent{{data: "{\n\"response\":{\"status\":\"completed\"}}", name: "response.completed"}}, got)
	}
}

func TestBufferedSSEReaderBoundsAggregateEvent(t *testing.T) {
	previous := constant.StreamScannerMaxBufferMB
	constant.StreamScannerMaxBufferMB = 1
	t.Cleanup(func() { constant.StreamScannerMaxBufferMB = previous })
	line := "data: " + strings.Repeat("x", 600<<10) + "\n"
	called := false
	err := ReadSSEEvents(strings.NewReader(line+line+"\n"), func(_, _ string) (bool, error) {
		called = true
		return false, nil
	})
	require.ErrorContains(t, err, "SSE event exceeds")
	assert.False(t, called)
}
