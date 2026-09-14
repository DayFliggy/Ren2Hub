package newapi_test

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/newapi"
	"github.com/QuantumNous/new-api/relay/channel/sub2api"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeminiURLUsesMappedModel(t *testing.T) {
	for _, provider := range []struct {
		name string
		kind int
		url  func(*relaycommon.RelayInfo) (string, error)
	}{
		{"newapi", constant.ChannelTypeNewAPI, (&newapi.Adaptor{}).GetRequestURL},
		{"sub2api", constant.ChannelTypeSub2API, (&sub2api.Adaptor{}).GetRequestURL},
	} {
		for _, test := range []struct {
			name, path, model, expected string
			format                      types.RelayFormat
		}{
			{"json", "/v1beta/models/public-alias:generateContent", "upstream-model", "/v1beta/models/upstream-model:generateContent", types.RelayFormatGemini},
			{"stream_query", "/v1beta/models/public-alias:streamGenerateContent?alt=sse", "upstream-model", "/v1beta/models/upstream-model:streamGenerateContent?alt=sse", types.RelayFormatGemini},
			{"reserved_characters", "/v1/models/public%20alias:generateContent", "upstream #1?", "/v1/models/upstream%20%231%3F:generateContent", types.RelayFormatGemini},
			{"responses_unchanged", "/v1/responses/compact?trace=1", "upstream-model", "/v1/responses/compact?trace=1", types.RelayFormatOpenAIResponses},
			{"invalid_path", "/v1beta/models/public-alias", "upstream-model", "", types.RelayFormatGemini},
			{"missing_model", "/v1beta/models/public-alias:generateContent", "", "", types.RelayFormatGemini},
		} {
			t.Run(provider.name+"/"+test.name, func(t *testing.T) {
				info := &relaycommon.RelayInfo{RelayFormat: test.format, RequestURLPath: test.path, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: provider.kind, ChannelBaseUrl: "https://gateway.invalid/prefix", UpstreamModelName: test.model}}
				actual, err := provider.url(info)
				if test.expected == "" {
					require.Error(t, err)
					assert.Empty(t, actual)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, "https://gateway.invalid/prefix"+test.expected, actual)
				assert.Equal(t, test.path, info.RequestURLPath)
			})
		}
	}
}
