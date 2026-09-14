package dify

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
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/sjson"
)

func TestDifyUploadRespectsGatewayLifecycle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, mode := range []string{"success", "admission", "auth_override", "upload_status", "upload_identity", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var uploaded, generated, prepared atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, int32(1), prepared.Load(), "admission must precede any provider I/O")
				assert.Equal(t, "Bearer channel-key", r.Header.Get("Authorization"))
				if r.URL.Path == "/v1/files/upload" {
					uploaded.Add(1)
					assert.NoError(t, r.ParseMultipartForm(1024*1024))
					assert.Equal(t, "final-user", r.FormValue("user"))
					file, _, err := r.FormFile("file")
					if assert.NoError(t, err) {
						data, readErr := io.ReadAll(file)
						assert.NoError(t, readErr)
						assert.NoError(t, file.Close())
						assert.Equal(t, "image-data", string(data))
					}
					switch mode {
					case "upload_status":
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = io.WriteString(w, `{"error":"upload unavailable"}`)
					case "upload_identity":
						_, _ = io.WriteString(w, `{"unexpected":"no identity"}`)
					case "cancelled":
						cancel()
						select {
						case <-r.Context().Done():
						case <-time.After(3 * time.Second):
							t.Error("upload cancellation did not reach provider")
						}
					default:
						w.WriteHeader(http.StatusCreated)
						_, _ = io.WriteString(w, `{"id":"file-local"}`)
					}
					return
				}
				generated.Add(1)
				assert.Equal(t, "/v1/chat-messages", r.URL.Path)
				var payload DifyChatRequest
				assert.NoError(t, common.DecodeJson(r.Body, &payload))
				assert.Equal(t, "final-user", payload.User)
				assert.Equal(t, "final-query", payload.Query)
				assert.Equal(t, []DifyFile{{Type: "image", TransferMode: "local_file", UploadFileId: "file-local"}}, payload.Files)
				_, _ = io.WriteString(w, `{"answer":"ok"}`)
			}))
			defer upstream.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ConversionPolicy: types.ConversionLossPolicySafe,
				ChannelMeta: &relaycommon.ChannelMeta{ApiType: constant.APITypeDify, ChannelBaseUrl: upstream.URL, ApiKey: "channel-key"}}
			request := &dto.GeneralOpenAIRequest{}
			require.NoError(t, common.Unmarshal([]byte(`{"model":"dify","messages":[{"role":"user","content":[{"type":"text","text":"original-query"},{"type":"image_url","image_url":{"url":"data:image/png;base64,aW1hZ2UtZGF0YQ=="}}]}]}`), request))
			adaptor := &Adaptor{}
			adaptor.Init(info)
			converted, err := adaptor.ConvertOpenAIRequest(c, info, request)
			require.NoError(t, err)
			assert.Zero(t, uploaded.Load(), "conversion must be pure")
			data, err := common.Marshal(converted)
			require.NoError(t, err)
			data, err = sjson.SetBytes(data, "user", "final-user")
			require.NoError(t, err)
			data, err = sjson.SetBytes(data, "query", "final-query")
			require.NoError(t, err)
			info.BeforeSend = func(body io.Reader) error {
				prepared.Add(1)
				actual, err := io.ReadAll(body)
				assert.NoError(t, err)
				assert.JSONEq(t, string(data), string(actual))
				if mode == "admission" {
					return errors.New("capacity exhausted")
				}
				return nil
			}
			if mode == "auth_override" {
				info.ChannelMeta.HeadersOverride = map[string]any{"Authorization": "Bearer other-key"}
			}
			resp, err := adaptor.sendRequest(c, info, strings.NewReader(string(data)))
			if resp != nil {
				defer resp.Body.Close()
			}
			switch mode {
			case "success":
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, http.StatusOK, resp.StatusCode)
				assert.EqualValues(t, 1, generated.Load())
			case "upload_status":
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
			default:
				require.Error(t, err)
				assert.Nil(t, resp)
			}
			if mode == "auth_override" || mode == "admission" {
				assert.Zero(t, uploaded.Load())
				assert.False(t, info.UpstreamSent)
			} else {
				assert.EqualValues(t, 1, uploaded.Load())
				assert.True(t, info.UpstreamSideEffecting)
				classification := service.ClassifyAttemptOutcome(service.AttemptOutcome{Origin: service.AttemptOriginTransport, SideEffecting: info.UpstreamSideEffecting, Acceptance: service.AttemptAcceptanceUnknown})
				assert.False(t, classification.Retryable)
				assert.False(t, classification.Failoverable)
			}
			if mode != "success" {
				assert.Zero(t, generated.Load())
			}
		})
	}
}
