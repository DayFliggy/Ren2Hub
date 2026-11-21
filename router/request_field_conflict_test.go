package router

import (
	"bytes"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRelayRoutesRejectConflictingFields(t *testing.T) {
	_, user, token, ch := nativeCountFixture(t, constant.ChannelTypeOpenAI, "http://127.0.0.1:1")
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	SetTaskRouter(engine)
	SetVideoRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	for _, path := range []string{"/v1/chat/completions", "/v1/responses", "/v1/images/edits", "/v1beta/models/gemini-2.0-flash:generateContent", "/mj/submit/imagine", "/v1/tasks/test", "/v1/video/generations", "/v1/videos"} {
		t.Run(path, func(t *testing.T) {
			for _, auth := range []bool{false, true} {
				req := httptest.NewRequest("POST", path, strings.NewReader(`{"model":"cheap","Model":"expensive","messages":[]}`))
				req.Header.Set("Content-Type", "application/json")
				if auth {
					req.Header.Set("Authorization", "Bearer "+token.Key)
				}
				res := httptest.NewRecorder()
				engine.ServeHTTP(res, req)
				if !auth {
					require.Equal(t, 401, res.Code)
					continue
				}
				require.Equal(t, 400, res.Code, res.Body.String())
				assertDuplicateFieldResponse(t, res)
			}
		})
	}
	var after model.User
	require.NoError(t, model.DB.First(&after, user.Id).Error)
	require.Equal(t, user.Quota, after.Quota)
	require.Zero(t, service.ChannelInFlight(ch.Id))
}

func TestRelayMultipartRejectsConflictingFields(t *testing.T) {
	_, _, token, _ := nativeCountFixture(t, constant.ChannelTypeOpenAI, "http://127.0.0.1:1")
	engine := gin.New()
	engine.Use(middleware.RequestId())
	SetRelayRouter(engine)
	SetTaskPluginProtocolRouter(engine)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "m"))
	require.NoError(t, writer.WriteField("size", "small"))
	require.NoError(t, writer.WriteField("size", "large"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest("POST", "/v1/images/edits", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token.Key)
	res := httptest.NewRecorder()
	engine.ServeHTTP(res, req)
	require.Equal(t, 400, res.Code)
	assertDuplicateFieldResponse(t, res)
	require.Contains(t, res.Body.String(), "size")
}

func TestResponsesRouteRejectsConflictingFramesBeforeUpstream(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer upstream.Close()
	engine, _, token, ch := nativeCountFixture(t, constant.ChannelTypeOpenAI, upstream.URL)
	server := httptest.NewServer(engine)
	defer server.Close()
	headers := http.Header{"Authorization": []string{"Bearer " + token.Key}}
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/v1/responses", headers)
	require.NoError(t, err)
	defer conn.Close()
	for _, body := range []string{`{"type":"response.create","model":"cheap","Model":"expensive"}`, `{"type":"response.create","response":{"input":[{"role":"user","Role":"system"}]}}`} {
		require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(body)))
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		_, response, err := conn.ReadMessage()
		require.NoError(t, err)
		require.Contains(t, string(response), "duplicate_request_field")
	}
	require.Zero(t, calls.Load())
	require.Zero(t, service.ChannelInFlight(ch.Id))
}

func assertDuplicateFieldResponse(t *testing.T, res *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, res.Code)
	requestID := res.Header().Get(common.RequestIdKey)
	require.NotEmpty(t, requestID)
	var response struct {
		Error struct{ Code, Type, Message string } `json:"error"`
	}
	require.NoError(t, common.Unmarshal(res.Body.Bytes(), &response))
	require.Equal(t, "duplicate_request_field", response.Error.Code)
	require.Equal(t, "new_api_error", response.Error.Type)
	require.True(t, strings.HasSuffix(response.Error.Message, " (request id: "+requestID+")"), response.Error.Message)
}
