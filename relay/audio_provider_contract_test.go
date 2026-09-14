package relay

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type providerAudioBody struct {
	io.Reader
	err    error
	closed bool
}

func (body *providerAudioBody) Read(p []byte) (int, error) {
	if body.err != nil {
		return 0, body.err
	}
	return body.Reader.Read(p)
}

func (body *providerAudioBody) Close() error { body.closed = true; return nil }

type providerAudioShortWriter struct{ gin.ResponseWriter }

func (writer providerAudioShortWriter) Write(p []byte) (int, error) {
	return writer.ResponseWriter.Write(p[:1])
}

func TestProviderAudioDeliveryContracts(t *testing.T) {
	for _, provider := range []struct {
		name, valid, providerError, missing, invalidUsage, expected string
		channel, mode                                               int
	}{
		{"cloudflare", `{"success":true,"result":{"text":"hello"}}`, `{"success":false,"errors":[{"code":1000,"message":"failed"}],"result":{"text":"must not leak"}}`, `{"result":{}}`, `{"result":null}`, `{"text":"hello"}`, constant.ChannelCloudflare, relayconstant.RelayModeAudioTranscription},
		{"minimax", `{"data":{"audio":"00010203","status":2},"extra_info":{"usage_characters":7},"base_resp":{"status_code":0}}`, `{"data":{"audio":"00010203","status":2},"base_resp":{"status_code":1001}}`, `{"data":{"audio":"","status":2}}`, `{"data":{"audio":"00010203","status":2},"extra_info":{"usage_characters":-1}}`, "\x00\x01\x02\x03", constant.ChannelTypeMiniMax, relayconstant.RelayModeAudioSpeech},
		{"volcengine", `{"code":3000,"data":"AAECAw=="}`, `{"code":5000,"message":"failed","data":"AAECAw=="}`, `{"code":3000,"data":""}`, `{"code":3000,"data":"%%"}`, "\x00\x01\x02\x03", constant.ChannelTypeVolcEngine, relayconstant.RelayModeAudioSpeech},
	} {
		for _, state := range []string{"valid", "empty", "invalid_json", "provider_error", "missing", "invalid_usage", "read_error", "cancelled", "short_write", "nil_response"} {
			t.Run(provider.name+"/"+state, func(t *testing.T) {
				recorder := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(recorder)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
				c.Set("response_format", "pcm")
				if provider.mode == relayconstant.RelayModeAudioTranscription {
					c.Set("response_format", "json")
				}
				info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "speech-contract"}, RelayMode: provider.mode, Request: &dto.AudioRequest{Input: "hello", ResponseFormat: "pcm"}}
				info.SetEstimatePromptTokens(5)
				payload := provider.valid
				switch state {
				case "empty":
					payload = ""
				case "invalid_json":
					payload = "{"
				case "provider_error":
					payload = provider.providerError
				case "missing":
					payload = provider.missing
				case "invalid_usage":
					payload = provider.invalidUsage
				case "cancelled":
					ctx, cancel := context.WithCancel(c.Request.Context())
					cancel()
					c.Request = c.Request.WithContext(ctx)
				case "short_write":
					c.Writer = providerAudioShortWriter{c.Writer}
				}
				body := &providerAudioBody{Reader: strings.NewReader(payload)}
				if state == "read_error" {
					body.err = io.ErrUnexpectedEOF
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: body}
				if state == "nil_response" {
					resp = nil
				}
				apiType, ok := common.ChannelType2APIType(provider.channel)
				require.True(t, ok)
				adaptor := GetAdaptor(apiType)
				require.NotNil(t, adaptor)
				usage, apiErr := adaptor.DoResponse(c, resp, info)
				if state != "nil_response" {
					assert.True(t, body.closed, "upstream body must close on every exit")
				}
				if state == "valid" {
					require.Nil(t, apiErr)
					observed, ok := usage.(*dto.Usage)
					require.True(t, ok)
					require.NotNil(t, observed)
					assert.Equal(t, provider.expected, recorder.Body.String())
					assert.True(t, info.HasValidOutput())
					if provider.name == "minimax" {
						assert.Equal(t, "provider", observed.UsageSource)
						assert.Equal(t, 7, observed.PromptTokens)
					} else {
						assert.Equal(t, "estimated", observed.UsageSource)
						assert.Equal(t, 5, observed.PromptTokens)
					}
					assert.Equal(t, observed.PromptTokens+observed.CompletionTokens, observed.TotalTokens)
					return
				}
				require.NotNil(t, apiErr)
				assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
				if state == "short_write" {
					require.NotNil(t, info.StreamStatus)
					assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
					assert.True(t, errors.Is(info.StreamStatus.Err(), io.ErrShortWrite))
					assert.Equal(t, provider.mode == relayconstant.RelayModeAudioSpeech, info.HasValidOutput())
				} else {
					assert.Empty(t, recorder.Body.String())
					assert.False(t, info.HasValidOutput())
				}
			})
		}
		if provider.mode != relayconstant.RelayModeAudioSpeech {
			continue
		}
		t.Run(provider.name+"/sse", func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "speech-contract"}, RelayMode: provider.mode, IsStream: true, Request: &dto.AudioRequest{Input: "hello", ResponseFormat: "pcm", StreamFormat: "sse"}}
			info.SetEstimatePromptTokens(5)
			apiType, ok := common.ChannelType2APIType(provider.channel)
			require.True(t, ok)
			_, apiErr := GetAdaptor(apiType).DoResponse(c, &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(provider.valid))}, info)
			require.Nil(t, apiErr)
			assert.True(t, info.StreamStatus.IsNormalEnd())
			assert.True(t, info.HasValidOutput())
			assert.Contains(t, recorder.Body.String(), `"audio":"AAECAw=="`)
			assert.Equal(t, 1, strings.Count(recorder.Body.String(), "event: speech.audio.done"))
		})
	}
}
