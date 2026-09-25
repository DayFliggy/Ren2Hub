package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicContentRevalidationTracksAdminChanges(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{"Notice": "欢迎 <hello>"}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
	router := gin.New()
	router.GET("/notice", GetNotice)
	request := func(etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/notice", nil)
		req.Header.Set("If-None-Match", etag)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	first := request("")
	require.Equal(t, http.StatusOK, first.Code)
	assert.JSONEq(t, `{"success":true,"message":"","data":"欢迎 <hello>"}`, first.Body.String())
	assert.Equal(t, "no-cache", first.Header().Get("Cache-Control"))
	assert.Equal(t, "Accept-Encoding", first.Header().Get("Vary"))
	etag := first.Header().Get("ETag")
	require.Contains(t, etag, `W/"`)
	unchanged := request(`"other", ` + etag)
	assert.Equal(t, http.StatusNotModified, unchanged.Code)
	assert.Empty(t, unchanged.Body.String())
	common.OptionMapRWMutex.Lock()
	common.OptionMap["Notice"] = "updated"
	common.OptionMapRWMutex.Unlock()
	changed := request(etag)
	assert.Equal(t, http.StatusOK, changed.Code)
	assert.NotEqual(t, etag, changed.Header().Get("ETag"))
}

func TestVolcEngineModelsUsesNativeAPIBase(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v3/models", r.URL.Path)
		assert.Equal(t, "Bearer fixture-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"doubao-model"}]}`))
	}))
	t.Cleanup(server.Close)
	channel := &model.Channel{Type: constant.ChannelTypeVolcEngine, BaseURL: &server.URL, Key: "fixture-key"}
	models, err := fetchChannelUpstreamModelIDs(channel)
	require.NoError(t, err)
	assert.Equal(t, []string{"doubao-model"}, models)
}
