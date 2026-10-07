package xunfei

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type xunfeiSignalWriter struct {
	*httptest.ResponseRecorder
	written chan struct{}
	once    sync.Once
}

func (w *xunfeiSignalWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(data)
	w.once.Do(func() { close(w.written) })
	return n, err
}

func TestXunfeiGatewayTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"json", "stream", "truncated_json", "truncated_stream", "provider_error", "malformed", "cancelled", "admission_rejected"} {
		t.Run(mode, func(t *testing.T) {
			stream := mode != "json" && mode != "truncated_json"
			var requests atomic.Int32
			received := make(chan []byte, 1)
			closed := make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if !assert.NoError(t, err) {
					return
				}
				defer conn.Close()
				defer close(closed)
				_, payload, err := conn.ReadMessage()
				if !assert.NoError(t, err) {
					return
				}
				received <- payload
				prefix := `{"header":{"code":0},"payload":{"choices":{"status":1,"text":[{"content":"matrix-response","role":"assistant","index":0}]},"usage":{"text":{"prompt_tokens":10,"completion_tokens":1,"total_tokens":11}}}}`
				if mode == "provider_error" {
					assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"header":{"code":10000,"message":"provider rejected"}}`)))
					return
				}
				assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(prefix)))
				if strings.HasPrefix(mode, "truncated") {
					return
				}
				if mode == "malformed" {
					assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{broken}`)))
					return
				}
				if mode == "cancelled" {
					_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
					_, _, _ = conn.ReadMessage()
					return
				}
				assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"header":{"code":0},"payload":{"choices":{"status":1,"text":[{"content":" complete","role":"assistant","index":0}]}}}`)))
				assert.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"header":{"code":0},"payload":{"choices":{"status":2,"text":[]},"usage":{"text":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}}}`)))
			}))
			defer provider.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorder := &xunfeiSignalWriter{ResponseRecorder: httptest.NewRecorder(), written: make(chan struct{})}
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions, IsStream: stream, DisablePing: true,
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeXunfei, ApiType: constant.APITypeXunfei, ApiKey: "test-app|test-secret|test-key", ChannelBaseUrl: provider.URL + "/spark", UpstreamModelName: "SparkDesk-v3.5"}}
			info.SetEstimatePromptTokens(10)
			adaptor := &Adaptor{}
			request := &dto.GeneralOpenAIRequest{Model: "SparkDesk-v3.5", Stream: &stream, Messages: []dto.Message{{Role: "user", Content: "prompt"}}}
			converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			body, err = sjson.SetBytes(body, "parameter.chat.temperature", 0)
			require.NoError(t, err)
			sends := 0
			info.BeforeSend = func(final io.Reader) error {
				sends++
				got, err := io.ReadAll(final)
				require.NoError(t, err)
				assert.JSONEq(t, string(body), string(got))
				if mode == "admission_rejected" {
					return errors.New("no channel capacity")
				}
				return nil
			}
			response, err := adaptor.DoRequest(c, info, bytes.NewReader(body))
			require.Equal(t, 1, sends)
			if mode == "admission_rejected" {
				require.ErrorContains(t, err, "no channel capacity")
				assert.Zero(t, requests.Load())
				assert.False(t, info.UpstreamSent)
				return
			}
			require.NoError(t, err)
			assert.True(t, info.UpstreamSent)
			if mode == "cancelled" {
				go func() {
					select {
					case <-recorder.written:
						cancel()
					case <-ctx.Done():
					}
				}()
			}
			result, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
			usage, ok := result.(*dto.Usage)
			require.True(t, ok)
			require.NotNil(t, usage)
			if mode == "json" || mode == "stream" {
				require.Nil(t, apiErr)
				assert.Equal(t, 12, usage.TotalTokens, "provider usage is cumulative, not additive")
				assert.True(t, info.StreamStatus.IsNormalEnd())
				assert.Contains(t, recorder.Body.String(), "matrix-response")
				if stream {
					assert.Contains(t, recorder.Body.String(), "[DONE]")
					assert.Contains(t, recorder.Body.String(), `"content":" complete"`)
					assert.Equal(t, 1, strings.Count(recorder.Body.String(), `"role":"assistant"`))
					assert.NotContains(t, recorder.Body.String(), `"index":1`)
				} else {
					assert.Equal(t, "matrix-response complete", gjson.Get(recorder.Body.String(), "choices.0.message.content").String())
				}
			} else {
				require.NotNil(t, apiErr)
				assert.False(t, info.StreamStatus.IsNormalEnd())
				assert.NotContains(t, recorder.Body.String(), "[DONE]")
				if mode == "truncated_json" || mode == "provider_error" {
					assert.Empty(t, recorder.Body.String())
				} else {
					assert.Contains(t, recorder.Body.String(), "matrix-response")
					assert.Equal(t, 11, usage.TotalTokens)
				}
			}
			select {
			case payload := <-received:
				assert.JSONEq(t, string(body), string(payload), "send the prepared body including overrides")
			case <-time.After(5 * time.Second):
				t.Fatal("provider did not receive the request")
			}
			select {
			case <-closed:
			case <-time.After(5 * time.Second):
				t.Fatal("provider websocket was not released")
			}
			assert.EqualValues(t, 1, requests.Load())
		})
	}
}
