package channel

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestHeaderPassthroughNeverForwardsWebSocketCredentials(t *testing.T) {
	for _, rule := range []string{"*", "re:.*", "regex:.*"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/v1/realtime", nil)
		c.Request.Header.Set("Sec-WebSocket-Protocol", "realtime, openai-insecure-api-key.FAKE_GATEWAY_KEY")
		c.Request.Header.Set("X-Trace", "trace")
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{HeadersOverride: map[string]any{rule: ""}}}
		headers, err := processHeaderOverride(info, c)
		require.NoError(t, err)
		assert.Empty(t, headers["sec-websocket-protocol"], rule)
		assert.Equal(t, "trace", headers["x-trace"])
		info.ChannelMeta.HeadersOverride["Sec-WebSocket-Protocol"] = "upstream-protocol"
		headers, err = processHeaderOverride(info, c)
		require.NoError(t, err)
		assert.Equal(t, "upstream-protocol", headers["sec-websocket-protocol"])
	}
}
