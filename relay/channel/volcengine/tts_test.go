package volcengine

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ttsAudioFrame(t *testing.T, seq int32, payload string) []byte {
	t.Helper()
	flag := MsgTypeFlagPositiveSeq
	if seq < 0 {
		flag = MsgTypeFlagNegativeSeq
	}
	msg, err := NewMessage(MsgTypeAudioOnlyServer, flag)
	require.NoError(t, err)
	msg.Sequence, msg.Payload = seq, []byte(payload)
	frame, err := msg.Marshal()
	require.NoError(t, err)
	return frame
}

func ttsTestContext(t *testing.T, baseURL string, stream bool) (*gin.Context, *httptest.ResponseRecorder, *relaycommon.RelayInfo) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL, ApiKey: "test-app|test-key", UpstreamModelName: "upstream-speech"}, RelayMode: relayconstant.RelayModeAudioSpeech, IsStream: stream, OriginModelName: "client-alias", Request: &dto.AudioRequest{Model: "client-alias", Input: "hello", Voice: "alloy", ResponseFormat: "pcm"}}
	info.SetEstimatePromptTokens(5)
	return c, recorder, info
}

func TestVolcTTSSocketLifecycle(t *testing.T) {
	message, err := NewMessage(MsgTypeError, MsgTypeFlagNoSeq)
	require.NoError(t, err)
	message.ErrorCode, message.Payload = 5000, []byte("upstream failed")
	errorFrame, err := message.Marshal()
	require.NoError(t, err)
	truncatedFrame := ttsAudioFrame(t, -1, "audio")
	truncatedFrame = truncatedFrame[:len(truncatedFrame)-1]
	for _, test := range []struct {
		name                string
		frames              [][]byte
		complete, delivered bool
	}{
		{"complete", [][]byte{ttsAudioFrame(t, 1, "first"), ttsAudioFrame(t, -2, "last")}, true, true},
		{"audio_ack", [][]byte{{0x11, 0xb0, 0x00, 0x00}, ttsAudioFrame(t, 1, "first"), ttsAudioFrame(t, -2, "last")}, true, true},
		{"normal_close_without_terminal", [][]byte{ttsAudioFrame(t, 1, "first")}, false, true},
		{"empty_close", nil, false, false},
		{"empty_terminal", [][]byte{ttsAudioFrame(t, -1, "")}, false, false},
		{"provider_error", [][]byte{errorFrame}, false, false},
		{"error_after_audio", [][]byte{ttsAudioFrame(t, 1, "first"), errorFrame}, false, true},
		{"truncated_payload", [][]byte{truncatedFrame}, false, false},
	} {
		for _, stream := range []bool{false, true} {
			name := test.name + "/binary"
			if stream {
				name = test.name + "/sse"
			}
			t.Run(name, func(t *testing.T) {
				var admission atomic.Int32
				serverErrors := make(chan error, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					if admission.Load() != 1 {
						serverErrors <- errors.New("WebSocket dial preceded send admission")
						return
					}
					conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
					if err != nil {
						serverErrors <- err
						return
					}
					defer conn.Close()
					_, err = ReceiveMessage(conn)
					if err != nil {
						serverErrors <- err
						return
					}
					for _, frame := range test.frames {
						if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
							serverErrors <- err
							return
						}
					}
					_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
					serverErrors <- nil
				}))
				defer server.Close()
				c, recorder, info := ttsTestContext(t, "ws"+strings.TrimPrefix(server.URL, "http"), stream)
				info.BeforeSend = func(body io.Reader) error {
					payload, err := io.ReadAll(body)
					require.NoError(t, err)
					var request VolcengineTTSRequest
					require.NoError(t, common.Unmarshal(payload, &request))
					assert.Equal(t, "upstream-speech", request.Request.Model)
					assert.Equal(t, "hello", request.Request.Text)
					assert.Equal(t, "test-app", request.App.AppID)
					admission.Add(1)
					return nil
				}
				adaptor := &Adaptor{}
				body, err := adaptor.ConvertAudioRequest(c, info, *info.Request.(*dto.AudioRequest))
				require.NoError(t, err)
				result, err := adaptor.DoRequest(c, info, body)
				require.NoError(t, err)
				resp, ok := result.(*http.Response)
				require.True(t, ok)
				require.NotNil(t, resp.Body)
				usage, apiErr := adaptor.DoResponse(c, resp, info)
				require.NoError(t, <-serverErrors)
				assert.True(t, info.UpstreamSent)
				assert.Equal(t, int32(1), admission.Load())
				assert.Equal(t, test.complete, apiErr == nil)
				assert.Equal(t, test.complete, info.StreamStatus.IsNormalEnd())
				assert.Equal(t, test.delivered, info.HasValidOutput())
				observed, ok := usage.(*dto.Usage)
				require.True(t, ok)
				assert.Equal(t, "estimated", observed.UsageSource)
				if test.delivered {
					assert.Equal(t, 5, observed.PromptTokens)
					assert.Equal(t, 5, observed.TotalTokens)
				} else {
					assert.Zero(t, observed.TotalTokens)
					assert.Empty(t, recorder.Body.String())
				}
				if stream {
					assert.Equal(t, test.complete, strings.Contains(recorder.Body.String(), "event: speech.audio.done"))
				} else if test.complete {
					assert.Equal(t, "firstlast", recorder.Body.String())
					assert.Equal(t, "audio/pcm", recorder.Header().Get("Content-Type"))
				}
			})
		}
	}
}

func TestVolcTTSAdmissionFailureDoesNotDial(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) { calls.Add(1) }))
	defer server.Close()
	c, _, info := ttsTestContext(t, "ws"+strings.TrimPrefix(server.URL, "http"), false)
	denied := errors.New("capacity denied")
	info.BeforeSend = func(io.Reader) error { return denied }
	adaptor := &Adaptor{}
	body, err := adaptor.ConvertAudioRequest(c, info, *info.Request.(*dto.AudioRequest))
	require.NoError(t, err)
	_, err = adaptor.DoRequest(c, info, body)
	require.ErrorIs(t, err, denied)
	assert.Zero(t, calls.Load())
	assert.False(t, info.UpstreamSent)
}

func TestVolcTTSCancellationClosesBlockedSocket(t *testing.T) {
	requestRead := make(chan struct{})
	socketClosed := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
		if err != nil {
			socketClosed <- err
			return
		}
		defer conn.Close()
		if _, err := ReceiveMessage(conn); err != nil {
			socketClosed <- err
			return
		}
		close(requestRead)
		_, _, err = conn.ReadMessage()
		socketClosed <- err
	}))
	defer server.Close()
	c, recorder, info := ttsTestContext(t, "ws"+strings.TrimPrefix(server.URL, "http"), false)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	adaptor := &Adaptor{}
	body, err := adaptor.ConvertAudioRequest(c, info, *info.Request.(*dto.AudioRequest))
	require.NoError(t, err)
	result, err := adaptor.DoRequest(c, info, body)
	require.NoError(t, err)
	completed := make(chan struct{})
	go func() { adaptor.DoResponse(c, result.(*http.Response), info); close(completed) }()
	<-requestRead
	cancel()
	select {
	case <-completed:
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not release the blocked speech receiver")
	}
	require.Error(t, <-socketClosed)
	assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
	assert.False(t, info.HasValidOutput())
	assert.Empty(t, recorder.Body.String())
}

func TestVolcTTSRequestPreservesMappingAndZeroSpeed(t *testing.T) {
	c, _, info := ttsTestContext(t, "http://example.invalid", false)
	zero := 0.0
	request := *info.Request.(*dto.AudioRequest)
	request.Speed = &zero
	request.Metadata = []byte(`{"app":{"appid":"other","token":"other"},"audio":{"enable_emotion":false,"emotion_scale":0},"request":{"model":"other","silence_duration":0,"extra_param":{"enable_latex_tn":false,"cache_config":{"use_cache":false}}}}`)
	body, err := (&Adaptor{}).ConvertAudioRequest(c, info, request)
	require.NoError(t, err)
	var converted VolcengineTTSRequest
	require.NoError(t, common.DecodeJson(body, &converted))
	assert.Equal(t, 0.0, converted.Audio.SpeedRatio)
	assert.Equal(t, "upstream-speech", converted.Request.Model)
	assert.Equal(t, "test-app", converted.App.AppID)
	assert.Equal(t, "test-key", converted.App.Token)
	require.NotNil(t, converted.Audio.EnableEmotion)
	assert.False(t, *converted.Audio.EnableEmotion)
	require.NotNil(t, converted.Audio.EmotionScale)
	assert.Zero(t, *converted.Audio.EmotionScale)
	require.NotNil(t, converted.Request.SilenceDuration)
	assert.Zero(t, *converted.Request.SilenceDuration)
	require.NotNil(t, converted.Request.ExtraParam.EnableLatexTn)
	assert.False(t, *converted.Request.ExtraParam.EnableLatexTn)
	require.NotNil(t, converted.Request.ExtraParam.CacheConfig.UseCache)
	assert.False(t, *converted.Request.ExtraParam.CacheConfig.UseCache)
	request.ResponseFormat = "flac"
	_, err = (&Adaptor{}).ConvertAudioRequest(c, info, request)
	require.Error(t, err)
}

type ttsInterruptedWriter struct {
	gin.ResponseWriter
	cancel context.CancelFunc
	short  bool
}

func (writer ttsInterruptedWriter) Write(p []byte) (int, error) {
	if writer.short {
		return writer.ResponseWriter.Write(p[:1])
	}
	n, err := writer.ResponseWriter.Write(p)
	writer.cancel()
	return n, err
}

func (writer ttsInterruptedWriter) WriteString(s string) (int, error) {
	return writer.Write([]byte(s))
}

func TestVolcTTSClientInterruptionRetainsOnlyDeliveredUsage(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, short := range []bool{false, true} {
			name := "binary/cancel"
			if stream {
				name = "sse/cancel"
			}
			if short {
				name = strings.TrimSuffix(name, "cancel") + "short_write"
			}
			t.Run(name, func(t *testing.T) {
				frame := ttsAudioFrame(t, 1, "audio")
				socketClosed := make(chan error, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
					conn, err := (&websocket.Upgrader{}).Upgrade(w, request, nil)
					if err != nil {
						socketClosed <- err
						return
					}
					defer conn.Close()
					if _, err := ReceiveMessage(conn); err != nil {
						socketClosed <- err
						return
					}
					if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
						socketClosed <- err
						return
					}
					_, _, err = conn.ReadMessage()
					socketClosed <- err
				}))
				defer server.Close()
				c, recorder, info := ttsTestContext(t, "ws"+strings.TrimPrefix(server.URL, "http"), stream)
				ctx, cancel := context.WithCancel(c.Request.Context())
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
				c.Writer = ttsInterruptedWriter{ResponseWriter: c.Writer, cancel: cancel, short: short}
				adaptor := &Adaptor{}
				body, err := adaptor.ConvertAudioRequest(c, info, *info.Request.(*dto.AudioRequest))
				require.NoError(t, err)
				result, err := adaptor.DoRequest(c, info, body)
				require.NoError(t, err)
				usage, apiErr := adaptor.DoResponse(c, result.(*http.Response), info)
				require.NotNil(t, apiErr)
				assert.Equal(t, relaycommon.StreamEndReasonClientGone, info.StreamStatus.EndReason)
				assert.NotContains(t, recorder.Body.String(), "speech.audio.done")
				delivered := !stream || !short
				assert.Equal(t, delivered, info.HasValidOutput())
				observed, ok := usage.(*dto.Usage)
				require.True(t, ok)
				if delivered {
					assert.Equal(t, 5, observed.TotalTokens)
				} else {
					assert.Zero(t, observed.TotalTokens)
				}
				require.Error(t, <-socketClosed)
			})
		}
	}
}
