package claude_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupRequestHeaderNormalizesAnthropicBetaList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "empty items and padding are dropped",
			lines: []string{"a, ", " b"},
			want:  []string{"a,b"},
		},
		{
			name:  "duplicates keep first-seen order",
			lines: []string{"b,a", "a, c ,b"},
			want:  []string{"b,a,c"},
		},
		{
			name:  "only separators send no header",
			lines: []string{" , ", ","},
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			for _, line := range tt.lines {
				c.Request.Header.Add("anthropic-beta", line)
			}
			info := &relaycommon.RelayInfo{
				OriginModelName: "claude-opus-5-5",
				ChannelMeta:     &relaycommon.ChannelMeta{ApiKey: "sk-test"},
			}

			header := http.Header{}
			require.NoError(t, (&claude.Adaptor{}).SetupRequestHeader(c, &header, info))
			assert.Equal(t, tt.want, header.Values("anthropic-beta"))
		})
	}
}

// A beta sent on a second header line reaches the upstream, so billing
// header() must see it and apply the matching surcharge.
func TestBillingHeadersMatchUpstreamAnthropicBeta(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Add("anthropic-beta", "context-1m-2025-08-07, ")
	c.Request.Header.Add("anthropic-beta", " fast-mode-2026-02-01")

	info := relaycommon.GenRelayInfoClaude(c, &dto.ClaudeRequest{Model: "claude-opus-5-5"})
	info.ChannelMeta = &relaycommon.ChannelMeta{ApiKey: "sk-test"}
	upstream := http.Header{}
	require.NoError(t, (&claude.Adaptor{}).SetupRequestHeader(c, &upstream, info))

	input, err := helper.ResolveIncomingBillingExprRequestInput(c, info)
	require.NoError(t, err)
	assert.Equal(t, "context-1m-2025-08-07,fast-mode-2026-02-01", upstream.Get("anthropic-beta"))

	value, _, err := billingexpr.RunExprWithRequest(
		`header("anthropic-beta") == "`+upstream.Get("anthropic-beta")+`" && header("anthropic-beta") contains "fast-mode-2026-02-01" ? p * 6 : p`,
		billingexpr.TokenParams{P: 100},
		input,
	)
	require.NoError(t, err)
	assert.Equal(t, float64(600), value)
}
