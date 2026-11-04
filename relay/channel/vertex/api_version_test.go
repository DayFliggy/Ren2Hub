package vertex

import (
	"net/url"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVertexAPIVersionOnlyChangesGoogleModelPaths(t *testing.T) {
	for _, keyType := range []dto.VertexKeyType{"", dto.VertexKeyTypeAPIKey} {
		for _, model := range []string{"gemini-4-argon", "imagen-4.0-generate-001", "claude-sonnet-4", "llama-3.3-70b"} {
			if keyType == dto.VertexKeyTypeAPIKey && strings.HasPrefix(model, "llama") {
				continue
			}
			for _, version := range []string{"", "v1", "v1beta1"} {
				t.Run(string(keyType)+"/"+model+"/"+version, func(t *testing.T) {
					for _, region := range []string{"global", "us-central1"} {
						for _, base := range []string{"", "https://vertex.example"} {
							info := &relaycommon.RelayInfo{OriginModelName: model, ChannelMeta: &relaycommon.ChannelMeta{
								UpstreamModelName: model, ApiVersion: `{"default":"` + region + `"}`, ApiKey: `{"project_id":"project"}`, ChannelBaseUrl: base,
								ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: keyType, VertexProjectID: "project", VertexAPIVersion: version},
							}}
							if keyType == dto.VertexKeyTypeAPIKey {
								info.ApiKey = "test-key"
							}
							adaptor := &Adaptor{}
							adaptor.Init(info)
							got, err := adaptor.GetRequestURL(info)
							require.NoError(t, err)
							parsed, err := url.Parse(got)
							require.NoError(t, err)
							wantVersion := "v1"
							if adaptor.RequestMode == RequestModeOpenSource {
								wantVersion = "v1beta1"
							}
							if adaptor.RequestMode == RequestModeGemini && version != "" {
								wantVersion = version
							}
							assert.True(t, strings.HasPrefix(parsed.Path, "/"+wantVersion+"/"), got)
							assert.Contains(t, parsed.Path, "/projects/project/locations/"+region+"/")
							if keyType == dto.VertexKeyTypeAPIKey {
								assert.Equal(t, "test-key", parsed.Query().Get("key"))
							}
							if base != "" {
								assert.Equal(t, "vertex.example", parsed.Host)
							}
						}
					}
				})
			}
		}
	}
}

func TestVertexAPIVersionSupportsExpressStreaming(t *testing.T) {
	info := &relaycommon.RelayInfo{IsStream: true, OriginModelName: "gemini-4-argon", ChannelMeta: &relaycommon.ChannelMeta{
		UpstreamModelName: "gemini-4-argon", ApiVersion: `{"default":"global"}`, ApiKey: "test-key",
		ChannelOtherSettings: dto.ChannelOtherSettings{VertexKeyType: dto.VertexKeyTypeAPIKey, VertexAPIVersion: "v1beta1"},
	}}
	adaptor := &Adaptor{}
	adaptor.Init(info)
	got, err := adaptor.GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://aiplatform.googleapis.com/v1beta1/publishers/google/models/gemini-4-argon:streamGenerateContent?alt=sse&key=test-key", got)
}
