package helper

import (
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/kitutil"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real DTO still performs each mutation. The spy exposes the native direct
// self-map early return even when an equal model assignment would be invisible.
type mappingRequestSpy struct {
	*dto.GeneralOpenAIRequest
	setterCalls int
}

func (r *mappingRequestSpy) SetModelName(name string) {
	r.setterCalls++
	r.GeneralOpenAIRequest.SetModelName(name)
}

func TestModelMappedHelperPreservesNativeStateAndSetter(t *testing.T) {
	const origin, oldUpstream, bodyModel = "public", "previous-upstream", "body-model"
	for _, initial := range []bool{false, true} {
		withoutEdgeUpstream := oldUpstream
		if initial {
			withoutEdgeUpstream = origin
		}
		cases := []struct {
			name, origin, mapping, upstream string
			mapped                          bool
			setCalls                        int
			err                             string
		}{
			{"absent", origin, "", oldUpstream, initial, 1, ""},
			{"empty_object", origin, "{}", oldUpstream, initial, 1, ""},
			{"spaced_empty_object", origin, " { } ", withoutEdgeUpstream, initial, 1, ""},
			{"null", origin, "null", withoutEdgeUpstream, initial, 1, ""},
			{"unmatched", origin, `{"other":"target"}`, withoutEdgeUpstream, initial, 1, ""},
			{"empty_target", origin, `{"public":""}`, withoutEdgeUpstream, initial, 1, ""},
			{"origin_self", origin, `{"public":"public"}`, oldUpstream, false, 0, ""},
			{"chain", origin, `{"public":"mid","mid":"target"}`, "target", true, 1, ""},
			{"chain_tail_self", origin, `{"public":"mid","mid":"mid"}`, "mid", true, 1, ""},
			{"cycle", origin, `{"public":"mid","mid":"public"}`, oldUpstream, true, 0, "model_mapping_contains_cycle"},
			{"malformed_json", origin, `{"public":`, oldUpstream, initial, 0, "unmarshal_model_mapping_failed"},
			{"wrong_value_type", origin, `{"public":12}`, oldUpstream, initial, 0, "unmarshal_model_mapping_failed"},
			{"reasoning_base", "public@effort:high", `{"public":"target"}`, "target", true, 1, ""},
			{"reasoning_exact", "public@effort:high", `{"public@effort:high":"exact","public":"fallback"}`, "exact", true, 1, ""},
			{"reasoning_empty_exact", "public@effort:high", `{"public@effort:high":"","public":"fallback"}`, "fallback", true, 1, ""},
			{"reasoning_origin_self", "public@effort:high", `{"public":"public@effort:high"}`, oldUpstream, false, 0, ""},
		}
		for _, tc := range cases {
			for _, nilRequest := range []bool{false, true} {
				t.Run("initial_"+strconv.FormatBool(initial)+"/"+tc.name+"/nil_"+strconv.FormatBool(nilRequest), func(t *testing.T) {
					c, _ := gin.CreateTestContext(httptest.NewRecorder())
					c.Set("model_mapping", tc.mapping)
					info := &relaycommon.RelayInfo{OriginModelName: tc.origin, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: oldUpstream, IsModelMapped: initial}}
					spy := &mappingRequestSpy{GeneralOpenAIRequest: &dto.GeneralOpenAIRequest{Model: bodyModel}}
					var request dto.Request = spy
					if nilRequest {
						request = nil
					}
					err := ModelMappedHelper(c, info, request)
					if tc.err == "" {
						assert.NoError(t, err)
					} else {
						assert.EqualError(t, err, tc.err)
					}
					assert.Equal(t, tc.mapped, info.IsModelMapped, "native mapping state")
					assert.Equal(t, tc.upstream, info.UpstreamModelName, "native upstream identity")
					calls, wantBody := tc.setCalls, bodyModel
					if nilRequest {
						calls = 0
					} else if calls > 0 {
						wantBody = tc.upstream
					}
					assert.Equal(t, calls, spy.setterCalls, "native setter calls")
					assert.Equal(t, wantBody, spy.Model, "real request DTO model")
				})
			}
		}
	}
	t.Run("nil_channel_meta_origin_self", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("model_mapping", `{"public":"public"}`)
		info := &relaycommon.RelayInfo{OriginModelName: origin}
		spy := &mappingRequestSpy{GeneralOpenAIRequest: &dto.GeneralOpenAIRequest{Model: bodyModel}}
		require.NoError(t, ModelMappedHelper(c, info, spy))
		require.NotNil(t, info.ChannelMeta)
		assert.False(t, info.IsModelMapped)
		assert.Empty(t, info.UpstreamModelName)
		assert.Zero(t, spy.setterCalls)
		assert.Equal(t, bodyModel, spy.Model)
	})
}

type mappingConfiguredCodec struct {
	calls  int
	reject bool
}

func (c *mappingConfiguredCodec) Marshal(v any) ([]byte, error) { return json.Marshal(v) }
func (c *mappingConfiguredCodec) Decode(r io.Reader, v any) error {
	return json.NewDecoder(r).Decode(v)
}
func (c *mappingConfiguredCodec) Valid(raw []byte) bool { return json.Valid(raw) }
func (c *mappingConfiguredCodec) Unmarshal(raw []byte, v any) error {
	c.calls++
	if c.reject {
		return errors.New("configured codec rejection")
	}
	return json.Unmarshal(raw, v)
}

func TestModelMappingUsesConfiguredJSONCodec(t *testing.T) {
	const childEnv = "REN2HUB_MAPPING_CODEC_TEST_CHILD"
	if os.Getenv(childEnv) != "1" {
		binary, err := os.Executable()
		require.NoError(t, err)
		// SetCodec is startup-only and unsynchronized. Exercise it in a fresh
		// process so no parallel test or later request inherits this test codec.
		cmd := exec.Command(binary, "-test.run=^TestModelMappingUsesConfiguredJSONCodec$", "-test.timeout=20s")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "%s", output)
		return
	}
	codec := &mappingConfiguredCodec{}
	kitutil.SetCodec(codec)
	for _, reject := range []bool{false, true} {
		t.Run("reject_"+strconv.FormatBool(reject), func(t *testing.T) {
			codec.reject = reject
			before := codec.calls
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("model_mapping", `{"public":"target"}`)
			info := &relaycommon.RelayInfo{OriginModelName: "public", ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "previous-upstream"}}
			spy := &mappingRequestSpy{GeneralOpenAIRequest: &dto.GeneralOpenAIRequest{Model: "body-model"}}
			err := ModelMappedHelper(c, info, spy)
			assert.Equal(t, before+1, codec.calls, "configured decoder must parse the mapping once")
			if reject {
				assert.EqualError(t, err, "unmarshal_model_mapping_failed")
				assert.False(t, info.IsModelMapped)
				assert.Equal(t, "previous-upstream", info.UpstreamModelName)
				assert.Zero(t, spy.setterCalls)
				assert.Equal(t, "body-model", spy.Model)
			} else {
				assert.NoError(t, err)
				assert.True(t, info.IsModelMapped)
				assert.Equal(t, "target", info.UpstreamModelName)
				assert.Equal(t, 1, spy.setterCalls)
				assert.Equal(t, "target", spy.Model)
			}
		})
	}
}
