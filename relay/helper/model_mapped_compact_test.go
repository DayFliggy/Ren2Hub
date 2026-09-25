package helper

import (
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"testing"
)

func TestModelMappedHelperUsesOrdinaryIdentityForCompact(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []int{relayconstant.RelayModeResponses, relayconstant.RelayModeResponsesCompact} {
		for _, test := range []struct{ name, mapping, want string }{
			{"ordinary", `{"client":"middle","middle":"provider"}`, "provider"},
			{"literal suffix", `{"client":"provider-openai-compact"}`, "provider-openai-compact"},
			{"unmapped", `{}`, "client"},
		} {
			t.Run(test.name, func(t *testing.T) {
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Set("model_mapping", test.mapping)
				info := &relaycommon.RelayInfo{RelayMode: mode, OriginModelName: "client", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "client"}}
				request := &dto.OpenAIResponsesRequest{Model: "client"}
				require.NoError(t, ModelMappedHelper(ctx, info, request))
				assert.Equal(t, test.want, request.Model)
				assert.Equal(t, test.want, info.ActualUpstreamModelName())
				assert.Equal(t, "client", info.BillingModelName())
			})
		}
	}
}
