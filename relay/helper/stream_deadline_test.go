package helper

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamFirstOutputDeadlineCannotBeExtended(t *testing.T) {
	for name, frame := range map[string]string{
		"heartbeats":       ": heartbeat\n\n",
		"unfinished_event": "data: {\n",
		"metadata":         "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n",
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reader, writer := io.Pipe()
				writerDone := make(chan struct{})
				go func() {
					defer close(writerDone)
					defer writer.Close()
					for {
						if _, err := io.WriteString(writer, frame); err != nil {
							return
						}
						time.Sleep(200 * time.Millisecond)
					}
				}()
				c, resp, info := setupStreamTest(t, reader)
				resp.Body = reader
				start := time.Now()
				streamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
					require.NoError(t, StringData(c, data))
				}, false, streamTimeouts{firstOutput: time.Second, idle: 500 * time.Millisecond})
				<-writerDone
				assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
				assert.ErrorContains(t, info.StreamStatus.Err(), "first output timeout")
				assert.WithinDuration(t, start.Add(time.Second), time.Now(), 200*time.Millisecond)
				assert.False(t, c.Writer.Written())
				assert.False(t, info.HasValidOutput())
			})
		})
	}
}

func TestStreamValidOutputReplacesFirstOutputDeadlineWithIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		go func() {
			defer writer.Close()
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"first\"}}]}\n\n")
			for range 8 {
				time.Sleep(250 * time.Millisecond)
				_, _ = io.WriteString(writer, ": heartbeat\n\n")
			}
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}()
		c, resp, info := setupStreamTest(t, reader)
		resp.Body = reader
		streamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
			require.NoError(t, StringData(c, data))
		}, false, streamTimeouts{firstOutput: time.Second, idle: 500 * time.Millisecond})
		assert.True(t, info.StreamStatus.IsNormalEnd())
		assert.True(t, info.HasValidOutput())
		assert.True(t, c.Writer.Written())
	})
}

func TestStreamIdleTimeoutAfterOutputKeepsDelivery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		defer writer.Close()
		go func() {
			_, _ = io.WriteString(writer, "data: {\"choices\":[{\"delta\":{\"content\":\"delivered\"}}]}\n\n")
		}()
		c, resp, info := setupStreamTest(t, reader)
		resp.Body = reader
		streamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
			require.NoError(t, StringData(c, data))
		}, false, streamTimeouts{firstOutput: time.Second, idle: 500 * time.Millisecond})
		assert.True(t, c.Writer.Written(), "the controller must retain the committed-response retry fence")
		assert.True(t, info.HasValidOutput())
		assert.Equal(t, relaycommon.StreamEndReasonTimeout, info.StreamStatus.EndReason)
		_, err := io.WriteString(writer, "late")
		require.ErrorIs(t, err, io.ErrClosedPipe)
	})
}

func TestStreamCancelDiscardsIncompleteSSEEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reader, writer := io.Pipe()
		defer writer.Close()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			_, _ = io.WriteString(writer, "data: {\"unfinished\":")
			time.Sleep(time.Second)
			cancel()
		}()
		c, resp, info := setupStreamTest(t, reader)
		c.Request = c.Request.WithContext(ctx)
		resp.Body = reader
		called := false
		streamScannerHandler(c, resp, info, func(string, *StreamResult) { called = true }, false, streamTimeouts{firstOutput: 5 * time.Second, idle: 5 * time.Second})
		assert.False(t, called)
		assert.False(t, c.Writer.Written())
		assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
		_, err := io.WriteString(writer, "late")
		require.ErrorIs(t, err, io.ErrClosedPipe)
	})
}

func TestValidatedLargeSemanticFrameCommitsBeforeWrite(t *testing.T) {
	payload := `{"type":"response.image_generation_call.partial_image","partial_image_b64":"` + strings.Repeat("a", MaxStreamPreludeBytes+1) + `"}`
	c, resp, info := setupStreamTest(t, strings.NewReader("data: "+payload+"\n\ndata: [DONE]\n\n"))
	StreamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
		semantic, _ := StreamPayloadState(data)
		require.True(t, semantic)
		require.NoError(t, CommitStreamOutput(c))
		require.NoError(t, StringData(c, data))
	})
	assert.True(t, info.StreamStatus.IsNormalEnd())
	assert.True(t, info.HasValidOutput())
	assert.True(t, c.Writer.Written())
}

func TestRejectedSemanticWriteDoesNotExposePrelude(t *testing.T) {
	const role = `{"choices":[{"delta":{"role":"assistant"}}]}`
	const text = `{"choices":[{"delta":{"content":"not delivered"}}]}`
	c, resp, info := setupStreamTest(t, strings.NewReader("data: "+role+"\n\ndata: "+text+"\n\n"))
	StreamScannerHandler(c, resp, info, func(data string, result *StreamResult) {
		if data == role {
			require.NoError(t, StringData(c, data))
			return
		}
		require.NoError(t, CommitStreamOutput(c))
		result.Stop(errors.New("output state binding rejected"))
	})
	assert.False(t, c.Writer.Written())
	assert.False(t, info.HasValidOutput())
	assert.ErrorContains(t, info.StreamStatus.Err(), "output state binding rejected")
}
