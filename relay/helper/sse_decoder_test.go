package helper

import (
	"io"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamScannerSSEEventFraming(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n", "\r"} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(newline, "\r", "CR"), "\n", "LF"), func(t *testing.T) {
			body := strings.Join([]string{
				": heartbeat", "event: first", "id: 9", "retry: 1000",
				"data: {", "data:   \"value\": \"first\"}", "",
				"event: discarded", "", "data:second", "", "data:", "",
				"event: last", "data: tail",
			}, newline)
			c, resp, info := setupStreamTest(t, strings.NewReader(body))
			var got []sseEvent
			StreamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
				got = append(got, sseEvent{data: data, name: result.EventName()})
			})
			assert.Equal(t, []sseEvent{
				{data: "{\n  \"value\": \"first\"}", name: "first"},
				{data: "second"},
				{data: "tail", name: "last"},
			}, got)
			assert.ErrorIs(t, info.StreamStatus.Err(), io.ErrUnexpectedEOF)
			assert.False(t, c.Writer.Written())
		})
	}
}

func TestSSEEventSizeLimitIncludesAllDataLines(t *testing.T) {
	decoder := sseDecoder{maxSize: 12}
	event, err := decoder.addLine("data: 123456")
	require.NoError(t, err)
	require.Nil(t, event)
	_, err = decoder.addLine("data: 123456")
	require.ErrorContains(t, err, "SSE event exceeds 12 bytes")
}

func TestNDJSONFramingPreservesLinePayload(t *testing.T) {
	c, resp, info := setupStreamTest(t, strings.NewReader("  {\"value\":1}  \r\n{\"value\":2}\n"))
	var got []string
	NDJSONStreamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
		got = append(got, data)
		result.MarkValidOutput()
		require.NoError(t, StringData(c, data))
		if len(got) == 2 {
			result.Done()
		}
	})
	assert.Equal(t, []string{"  {\"value\":1}  ", "{\"value\":2}"}, got)
	assert.Equal(t, relaycommon.StreamEndReasonDone, info.StreamStatus.EndReason)
	assert.True(t, info.HasValidOutput())
}
