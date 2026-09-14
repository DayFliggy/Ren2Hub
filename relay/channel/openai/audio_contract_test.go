package openai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAudioContractContext(t *testing.T, body, contentType string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *http.Response, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{"Content-Type": {contentType}}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, IsStream: stream, Request: &dto.AudioRequest{ResponseFormat: "pcm"}}
	info.SetEstimatePromptTokens(10)
	return c, recorder, resp, info
}

func TestOpenaiSpeechStreamContracts(t *testing.T) {
	const delta = `{"type":"speech.audio.delta","audio":"AAECAw=="}`
	const done = `{"type":"speech.audio.done","usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`
	for _, test := range []struct {
		name, frames, source string
		complete, delivered  bool
	}{
		{"complete", delta + "\n" + done, "provider", true, true},
		{"eof", delta, "estimated", false, true},
		{"generic_done_is_not_audio_done", delta + "\n[DONE]", "estimated", false, true},
		{"invalid_base64", `{"type":"speech.audio.delta","audio":"%%%"}`, "estimated", false, false},
		{"missing_audio", done, "estimated", false, false},
		{"negative_usage", delta + "\n" + `{"type":"speech.audio.done","usage":{"input_tokens":10,"output_tokens":-1,"total_tokens":9}}`, "estimated", false, true},
		{"overflow_usage", delta + "\n" + `{"type":"speech.audio.done","usage":{"input_tokens":2147483647,"output_tokens":1}}`, "estimated", false, true},
		{"invalid_json", "{", "estimated", false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := ""
			for _, frame := range strings.Split(test.frames, "\n") {
				body += "data: " + frame + "\n\n"
			}
			c, recorder, resp, info := newAudioContractContext(t, body, "text/event-stream", true)
			usage := OpenaiTTSHandler(c, resp, info)
			require.NotNil(t, usage)
			assert.Equal(t, test.complete, info.StreamStatus.IsNormalEnd())
			assert.Equal(t, test.delivered, info.HasValidOutput())
			assert.Equal(t, test.source, usage.UsageSource)
			assert.Equal(t, test.complete, strings.Contains(recorder.Body.String(), `event: speech.audio.done`))
			assert.NotContains(t, recorder.Body.String(), "[DONE]")
			if test.complete {
				assert.Equal(t, 20, usage.CompletionTokens)
				assert.Equal(t, 20, usage.CompletionTokenDetails.AudioTokens)
				assert.Equal(t, 30, usage.TotalTokens)
			} else {
				require.NotNil(t, helper.StreamError(info))
			}
			if !test.delivered {
				assert.Empty(t, recorder.Body.String())
				assert.Zero(t, usage.TotalTokens)
			}
		})
	}
}

type audioInterruptedBody struct{ read bool }

func (r *audioInterruptedBody) Read(b []byte) (int, error) {
	if !r.read {
		r.read = true
		return copy(b, "partial audio"), nil
	}
	return 0, io.ErrUnexpectedEOF
}
func (r *audioInterruptedBody) Close() error { return nil }

func TestOpenaiAudioBinaryContracts(t *testing.T) {
	for _, scenario := range []string{"complete", "empty", "truncated", "json_error", "cancelled", "nil_response"} {
		t.Run(scenario, func(t *testing.T) {
			c, recorder, resp, info := newAudioContractContext(t, "\x00\x01\x02\x03", "audio/pcm", false)
			switch scenario {
			case "empty":
				resp.Body = io.NopCloser(strings.NewReader(""))
			case "truncated":
				resp.Body = &audioInterruptedBody{}
			case "json_error":
				resp.Header.Set("Content-Type", "application/json")
				resp.Body = io.NopCloser(strings.NewReader(`{"error":{"message":"failed"}}`))
			case "cancelled":
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			case "nil_response":
				resp = nil
			}
			usage := OpenaiTTSHandler(c, resp, info)
			assert.Equal(t, scenario == "complete", info.StreamStatus.IsNormalEnd())
			assert.Equal(t, scenario == "complete", info.HasValidOutput())
			if scenario == "complete" {
				assert.Equal(t, "\x00\x01\x02\x03", recorder.Body.String())
				assert.Positive(t, usage.CompletionTokenDetails.AudioTokens)
				assert.Equal(t, "estimated", usage.UsageSource)
			} else {
				assert.Empty(t, recorder.Body.String())
				assert.Zero(t, usage.TotalTokens)
				require.NotNil(t, helper.StreamError(info))
			}
			if scenario == "cancelled" {
				assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
			}
		})
	}
}

func TestOpenaiTranscriptionContracts(t *testing.T) {
	for _, test := range []struct {
		name, body, format string
		valid              bool
	}{
		{"json", `{"text":"hello","usage":{"input_tokens":10,"input_token_details":{"audio_tokens":8,"text_tokens":2},"output_tokens":2,"total_tokens":12}}`, "json", true},
		{"silence", `{"text":""}`, "json", true},
		{"text", "hello", "text", true},
		{"missing_text", `{}`, "json", false},
		{"negative_usage", `{"text":"hello","usage":{"prompt_tokens":-1,"input_tokens":10,"output_tokens":2,"total_tokens":12}}`, "json", false},
		{"negative_details", `{"text":"hello","usage":{"input_tokens":10,"input_token_details":{"audio_tokens":-1},"output_tokens":2,"total_tokens":12}}`, "json", false},
		{"inconsistent_details", `{"text":"hello","usage":{"input_tokens":10,"input_token_details":{"audio_tokens":20},"output_tokens":2,"total_tokens":12}}`, "json", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			contentType := "application/json"
			if test.format == "text" {
				contentType = "text/plain"
			}
			c, recorder, resp, info := newAudioContractContext(t, test.body, contentType, false)
			err, usage := OpenaiSTTHandler(c, resp, info, test.format)
			assert.Equal(t, test.valid, err == nil)
			assert.Equal(t, test.valid, info.HasValidOutput())
			if test.valid {
				assert.Equal(t, test.body, recorder.Body.String())
				require.NotNil(t, usage)
				if test.name == "json" {
					assert.Equal(t, 8, usage.PromptTokensDetails.AudioTokens)
					assert.Equal(t, 2, usage.CompletionTokenDetails.TextTokens)
					assert.Equal(t, "provider", usage.UsageSource)
				}
			} else {
				assert.Empty(t, recorder.Body.String())
			}
		})
	}
}

func TestOpenaiTranscriptionStreamCompletion(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			body := "data: {\"type\":\"transcript.text.delta\",\"delta\":\"hello\"}\n\n"
			if complete {
				body += "data: {\"type\":\"transcript.text.done\",\"text\":\"hello\",\"usage\":{\"input_tokens\":10,\"output_tokens\":2,\"total_tokens\":12}}\n\n"
			}
			c, recorder, resp, info := newAudioContractContext(t, body, "text/event-stream", false)
			err, usage := OpenaiSTTHandler(c, resp, info, "json")
			assert.Equal(t, complete, err == nil)
			assert.True(t, info.HasValidOutput())
			assert.True(t, info.IsStream)
			assert.Contains(t, recorder.Body.String(), "event: transcript.text.delta")
			assert.Equal(t, complete, strings.Contains(recorder.Body.String(), "event: transcript.text.done"))
			require.NotNil(t, usage)
			assert.Positive(t, usage.PromptTokensDetails.AudioTokens)
		})
	}
}
