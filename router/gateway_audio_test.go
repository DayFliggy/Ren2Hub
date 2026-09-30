package router

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupGatewayAudio(t *testing.T, handler http.HandlerFunc) (string, string) {
	t.Helper()
	url, key, _ := setupGatewaySDKWithUpstream(t, handler)
	require.NoError(t, configureGatewayFixturePrices(`{"gpt-4o-mini-tts":0.001,"gpt-4o-transcribe":0.001}`, false))
	for _, id := range []int{1, 4} {
		channel, err := model.GetChannelById(id, true)
		require.NoError(t, err)
		channel.Models = "gpt-4o-mini-tts,gpt-4o-transcribe"
		require.NoError(t, model.DB.Model(channel).Update("models", channel.Models).Error)
		require.NoError(t, channel.AddAbilities(model.DB))
	}
	// Keep a real reservation enabled instead of using the trust-quota bypass.
	require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", 1).Updates(map[string]any{"unlimited_quota": false, "available_quota": 2000}).Error)
	require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
	return url, key
}

func TestGatewayAudioSpeechAccounting(t *testing.T) {
	for _, scenario := range []string{"binary", "sse", "interrupted", "negative_usage", "empty", "truncated", "json_error", "invalid_input"} {
		t.Run(scenario, func(t *testing.T) {
			var sends atomic.Int32
			url, key := setupGatewayAudio(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/v1/audio/speech", r.URL.Path)
				var user model.User
				if assert.NoError(t, model.DB.First(&user, 1).Error) {
					assert.EqualValues(t, 9999500, user.Quota, "precharge precedes actual audio send")
				}
				for _, resource := range []string{service.UserAdmissionKey(1), service.ChannelAdmissionKey(1)} {
					count := service.ReadGatewayAdmissionRuntime(resource).InFlight
					if sends.Load() == 1 {
						assert.EqualValues(t, 1, count)
					}
				}
				switch scenario {
				case "sse", "interrupted", "negative_usage":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":\"AAECAw==\"}\n\n")
					if scenario == "sse" {
						_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"total_tokens\":30}}\n\n")
					} else if scenario == "negative_usage" {
						_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":10,\"output_tokens\":-1,\"total_tokens\":9}}\n\n")
					}
				case "json_error":
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"error":{"message":"upstream failed"}}`)
				default:
					w.Header().Set("Content-Type", "audio/pcm")
					if scenario == "truncated" {
						w.Header().Set("Content-Length", "20")
					}
					if scenario != "empty" {
						_, _ = w.Write([]byte{0, 1, 2, 3})
					}
				}
			})
			body := `{"model":"gpt-4o-mini-tts","input":"hello","voice":"alloy","response_format":"pcm"}`
			if scenario == "sse" || scenario == "interrupted" || scenario == "negative_usage" {
				body = `{"model":"gpt-4o-mini-tts","input":"hello","voice":"alloy","response_format":"pcm","stream_format":"sse"}`
			} else if scenario == "invalid_input" {
				body = `{"model":"gpt-4o-mini-tts","input":"","voice":"alloy"}`
			}
			code, output := postGatewayTask(t, url, key, "/v1/audio/speech", body)
			delivered := scenario == "binary" || scenario == "sse" || scenario == "interrupted" || scenario == "negative_usage"
			wantStatus, wantSends := http.StatusBadGateway, int32(2)
			if delivered {
				wantStatus, wantSends = http.StatusOK, 1
			}
			if scenario == "invalid_input" {
				wantStatus, wantSends = http.StatusBadRequest, 0
			}
			assert.Equal(t, wantStatus, code, output)
			assert.Equal(t, wantSends, sends.Load())
			if scenario == "interrupted" || scenario == "negative_usage" {
				assert.Contains(t, output, "speech.audio.delta")
				assert.NotContains(t, output, "speech.audio.done")
				assert.Contains(t, output, `"error"`)
			}
			if scenario == "binary" {
				assert.Equal(t, "\x00\x01\x02\x03", output)
			}
			awaitGatewayPermitsReleased(t)
			wantQuota, heldQuota := 0, 0
			settled := scenario == "binary" || scenario == "sse"
			if settled {
				wantQuota = 500
			} else if scenario != "invalid_input" {
				heldQuota = 500
			}
			require.Eventually(t, func() bool {
				var user model.User
				return model.DB.First(&user, 1).Error == nil && user.Quota == common.Quota(10000000-wantQuota-heldQuota)
			}, time.Second, 10*time.Millisecond)
			var user model.User
			require.NoError(t, model.DB.First(&user, 1).Error)
			assert.EqualValues(t, wantQuota, user.UsedQuota)
			assert.EqualValues(t, heldQuota, user.ReservedQuota)
			var logs []model.Log
			require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
			if settled {
				require.Len(t, logs, 1)
				assert.Equal(t, 1, user.RequestCount)
				if scenario == "sse" {
					assert.Equal(t, 20, logs[0].CompletionTokens)
				} else {
					assert.Zero(t, logs[0].CompletionTokens, "binary audio has no actual token count")
				}
			} else {
				assert.Empty(t, logs)
			}
			var failures int64
			require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
			assert.Equal(t, scenario != "binary" && scenario != "sse" && scenario != "invalid_input", failures > 0)
		})
	}
}

func TestGatewayAudioCancellationReleasesAdmission(t *testing.T) {
	stopped := make(chan struct{})
	finish := make(chan struct{})
	defer close(finish)
	var sends atomic.Int32
	url, key := setupGatewayAudio(t, func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"type\":\"speech.audio.delta\",\"audio\":\"AAECAw==\"}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-finish:
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url+"/v1/audio/speech", strings.NewReader(`{"model":"gpt-4o-mini-tts","input":"hello","voice":"alloy","response_format":"pcm","stream_format":"sse"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		require.NoError(t, err)
		if strings.HasPrefix(line, "data:") {
			break
		}
	}
	cancel()
	require.NoError(t, resp.Body.Close())
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream did not observe cancellation")
	}
	awaitGatewayPermitsReleased(t)
	require.Eventually(t, func() bool {
		var user model.User
		return model.DB.First(&user, 1).Error == nil && user.UsedQuota == 0 && user.ReservedQuota == 500 && user.Quota == 9999500
	}, time.Second, 10*time.Millisecond)
	var failures int64
	require.NoError(t, model.DB.Model(&model.ChannelHealth{}).Where("failure_count > 0").Count(&failures).Error)
	assert.Zero(t, failures)
	assert.EqualValues(t, 1, sends.Load())
}

func TestGatewayAudioTranscriptionRoundTrip(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			var sends atomic.Int32
			url, key := setupGatewayAudio(t, func(w http.ResponseWriter, r *http.Request) {
				sends.Add(1)
				assert.Equal(t, "/v1/audio/transcriptions", r.URL.Path)
				if assert.NoError(t, r.ParseMultipartForm(1<<20)) {
					assert.Equal(t, "gpt-4o-transcribe", r.FormValue("model"))
				}
				if stream {
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = io.WriteString(w, "data: {\"type\":\"transcript.text.delta\",\"delta\":\"hello\"}\n\n")
					_, _ = io.WriteString(w, "data: {\"type\":\"transcript.text.done\",\"text\":\"hello\",\"usage\":{\"input_tokens\":10,\"input_token_details\":{\"audio_tokens\":8,\"text_tokens\":2},\"output_tokens\":2,\"total_tokens\":12}}\n\n")
				} else {
					w.Header().Set("Content-Type", "application/json")
					_, _ = io.WriteString(w, `{"text":"hello","usage":{"input_tokens":10,"input_token_details":{"audio_tokens":8,"text_tokens":2},"output_tokens":2,"total_tokens":12}}`)
				}
			})
			if stream {
				var original model.Channel
				require.NoError(t, model.DB.First(&original, 1).Error)
				priority := int64(100)
				for range 64 {
					channel := model.Channel{Type: constant.ChannelCloudflare, Models: "gpt-4o-transcribe", Name: "nonstream-transcription", Key: "fixture", Status: common.ChannelStatusEnabled, BaseURL: original.BaseURL, Group: "default", CapacityTotal: 10, ChannelRatio: "1", Priority: &priority}
					require.NoError(t, model.DB.Create(&channel).Error)
					require.NoError(t, channel.AddAbilities(model.DB))
				}
				require.NoError(t, service.InitRouteCapabilityIndex(context.Background()))
			}
			body := &bytes.Buffer{}
			form := multipart.NewWriter(body)
			require.NoError(t, form.WriteField("model", "gpt-4o-transcribe"))
			require.NoError(t, form.WriteField("response_format", "json"))
			if stream {
				require.NoError(t, form.WriteField("stream", "true"))
			}
			part, err := form.CreateFormFile("file", "fixture.wav")
			require.NoError(t, err)
			wav := make([]byte, 44+48000)
			copy(wav[0:], "RIFF")
			binary.LittleEndian.PutUint32(wav[4:], uint32(len(wav)-8))
			copy(wav[8:], "WAVEfmt ")
			binary.LittleEndian.PutUint32(wav[16:], 16)
			binary.LittleEndian.PutUint16(wav[20:], 1)
			binary.LittleEndian.PutUint16(wav[22:], 1)
			binary.LittleEndian.PutUint32(wav[24:], 24000)
			binary.LittleEndian.PutUint32(wav[28:], 48000)
			binary.LittleEndian.PutUint16(wav[32:], 2)
			binary.LittleEndian.PutUint16(wav[34:], 16)
			copy(wav[36:], "data")
			binary.LittleEndian.PutUint32(wav[40:], 48000)
			_, err = part.Write(wav)
			require.NoError(t, err)
			require.NoError(t, form.Close())
			req, err := http.NewRequest(http.MethodPost, url+"/v1/audio/transcriptions", body)
			require.NoError(t, err)
			req.Header.Set("Content-Type", form.FormDataContentType())
			req.Header.Set("Authorization", "Bearer "+key)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()
			response, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, 200, resp.StatusCode, string(response))
			assert.Contains(t, string(response), "hello")
			if stream {
				assert.Contains(t, string(response), "event: transcript.text.done")
			}
			var user model.User
			require.Eventually(t, func() bool { return model.DB.First(&user, 1).Error == nil && user.UsedQuota == 500 }, time.Second, 10*time.Millisecond)
			assert.Equal(t, 1, user.RequestCount)
			assert.EqualValues(t, 1, sends.Load())
		})
	}
}
