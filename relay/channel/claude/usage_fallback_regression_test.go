package claude

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestClaudeFallbackPreservesCacheOnlyAndFillsMissingSides(t *testing.T) {
	for _, tt := range []struct {
		name                       string
		input, output, read, write int
		text                       string
		done                       bool
		wantInput                  int
		estimated                  bool
	}{
		{name: "cache-only explicit zero", write: 20000, done: true},
		{name: "broken cache read", read: 50000, text: "hello", output: 1, wantInput: 0, estimated: true},
		{name: "explicit zero with text", text: "hello", done: true, wantInput: 1200, estimated: true},
		{name: "output present input absent", output: 50, done: true, wantInput: 1200, estimated: true},
		{name: "fully reported", input: 1000, output: 50, done: true, wantInput: 1000},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatClaude, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-sonnet-5"}}
			info.SetEstimatePromptTokens(1200)
			usage := &dto.Usage{PromptTokens: tt.input, CompletionTokens: tt.output, PromptTokensDetails: dto.InputTokenDetails{CachedTokens: tt.read, CachedCreationTokens: tt.write}}
			state := &ClaudeResponseInfo{Usage: usage, Done: tt.done}
			state.ResponseText.WriteString(tt.text)
			require.Nil(t, HandleStreamFinalResponse(c, info, state))
			require.Equal(t, tt.wantInput, usage.PromptTokens)
			require.Equal(t, tt.read, usage.PromptTokensDetails.CachedTokens)
			require.Equal(t, tt.write, usage.PromptTokensDetails.CachedCreationTokens)
			if tt.estimated {
				require.Equal(t, "estimated_partial", usage.UsageSource)
			} else {
				require.NotEqual(t, "estimated_partial", usage.UsageSource)
				require.Equal(t, tt.output, usage.CompletionTokens)
			}
			require.NotNil(t, usage.BillingUsage)
			require.Equal(t, tt.estimated, usage.BillingUsage.Estimated)
		})
	}
}
