package helper

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestResolveMappedModelNameKeepsRoutingAndDispatchIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, origin, mapping, want, err string
		mapped                           bool
	}{
		{"none", "public", "", "public", "", false},
		{"empty_object", "public", "{}", "public", "", false},
		{"unmatched", "public", `{"other":"target"}`, "public", "", false},
		{"self", "public", `{"public":"public"}`, "public", "", false},
		{"chain", "public", `{"public":"mid","mid":"target"}`, "target", "", true},
		{"tail_self", "public", `{"public":"mid","mid":"mid"}`, "mid", "", true},
		{"reasoning_base", "public@effort:high", `{"public":"target"}`, "target", "", true},
		{"reasoning_self", "public@effort:high", `{"public":"public@effort:high"}`, "public@effort:high", "", false},
		{"cycle", "public", `{"public":"mid","mid":"public"}`, "", "model_mapping_contains_cycle", false},
		{"invalid", "public", `{"public":`, "", "unmarshal_model_mapping_failed", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name, mapped, err := relaycommon.ResolveMappedModelName(tc.origin, tc.mapping)
			if tc.err == "" {
				assert.NoError(t, err)
			} else {
				assert.EqualError(t, err, tc.err)
			}
			assert.Equal(t, tc.want, name, "routing/native-count identity")
			assert.Equal(t, tc.mapped, mapped, "existing public wrapper contract")
			if tc.err != "" {
				return
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("model_mapping", tc.mapping)
			info := &relaycommon.RelayInfo{OriginModelName: tc.origin, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: tc.origin}}
			request := &dto.GeneralOpenAIRequest{Model: tc.origin}
			assert.NoError(t, ModelMappedHelper(c, info, request))
			assert.Equal(t, name, info.UpstreamModelName, "dispatch and observation use one final identity")
			assert.Equal(t, mapped, info.IsModelMapped)
			assert.Equal(t, name, request.Model)
		})
	}
}
