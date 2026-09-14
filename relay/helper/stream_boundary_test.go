package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamPreludeFailureRemainsUncommitted(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"eof", "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n"},
		{"event limit", strings.Repeat("data: {\"type\":\"ping\"}\n", MaxStreamPreludeEvents+1)},
		{"byte limit", "data: {\"padding\":\"" + strings.Repeat("x", MaxStreamPreludeBytes) + "\"}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, resp, info := setupStreamTest(t, strings.NewReader(tc.body))
			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
				if err := StringData(c, data); err != nil {
					sr.Stop(err)
				}
			})
			require.NotNil(t, StreamError(info))
			assert.False(t, c.Writer.Written())
			assert.False(t, info.HasValidOutput())
		})
	}
}

func TestStreamTerminalCannotHideEarlierMalformedChunk(t *testing.T) {
	for _, semantic := range []bool{false, true} {
		t.Run(fmt.Sprint(semantic), func(t *testing.T) {
			prefix := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n"
			if semantic {
				prefix = "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n"
			}
			c, resp, info := setupStreamTest(t, strings.NewReader(prefix+"data: broken\ndata: [DONE]\n"))
			StreamScannerHandler(c, resp, info, func(data string, sr *StreamResult) {
				if data == "broken" {
					sr.Stop(fmt.Errorf("malformed frame"))
					return
				}
				if err := StringData(c, data); err != nil {
					sr.Stop(err)
				}
			})
			require.NotNil(t, StreamError(info))
			assert.Equal(t, semantic, c.Writer.Written())
			assert.Equal(t, semantic, info.HasValidOutput())
		})
	}
}
