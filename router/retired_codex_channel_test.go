package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestRetiredCodexManagementRoutesReturnNotFound(t *testing.T) {
	setupRelayRouterTestDB(t)
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()
	legacy := &model.Channel{Id: 57, Type: constant.ChannelTypeCodex, Name: "retired-history", Key: "legacy-fixture", BaseURL: &upstream.URL, Status: common.ChannelStatusEnabled, ChannelRatio: "1"}
	require.NoError(t, model.DB.Session(&gorm.Session{SkipHooks: true}).Create(legacy).Error)
	owner := &model.User{Username: "retired-codex-routes", AffCode: "retired-codex-routes", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	require.NoError(t, model.DB.Create(owner).Error)
	session := createRouterBrowserSession(t, owner.Id)
	engine := gin.New()
	SetApiRouter(engine)
	for _, route := range []struct{ method, path string }{
		{http.MethodPost, "/api/channel/57/codex/refresh"},
		{http.MethodGet, "/api/channel/57/codex/usage"},
		{http.MethodGet, "/api/channel/57/codex/usage/reset-credits"},
		{http.MethodPost, "/api/channel/57/codex/usage/reset"},
		{http.MethodGet, "/api/channel/57/provider-usage"},
		{http.MethodPost, "/api/channel/57/provider-usage/refresh"},
		{http.MethodPut, "/api/channel/57/provider-usage/settings"},
	} {
		request := httptest.NewRequest(route.method, route.path, strings.NewReader("{}"))
		request.Header.Set("Authorization", "Bearer "+session)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		assert.Equal(t, http.StatusNotFound, response.Code, route.method+" "+route.path)
	}
	assert.Zero(t, sends.Load())
	stored, err := model.GetChannelById(legacy.Id, true)
	require.NoError(t, err)
	assert.Equal(t, legacy.Key, stored.Key)
	assert.Equal(t, legacy.Status, stored.Status)
}

func TestRetiredCodexCannotSendThroughHistoricalCapabilitySnapshots(t *testing.T) {
	for _, snapshotType := range []int{constant.ChannelTypeCodex, constant.ChannelTypeOpenAI} {
		t.Run(constant.GetChannelTypeName(snapshotType), func(t *testing.T) {
			var sends atomic.Int32
			base, key, _ := setupGatewaySDKWithUpstream(t, func(w http.ResponseWriter, r *http.Request) { sends.Add(1); gatewaySDKUpstream(w, r) })
			// No refresh may hide a missing request-time lifecycle check in this test.
			require.NoError(t, model.DB.Model(&model.Channel{}).Where("id IN ?", []int{1, 4}).Updates(map[string]any{"type": constant.ChannelTypeCodex, "key": "legacy-subscription-fixture"}).Error)
			require.NoError(t, model.DB.Model(&model.ChannelModelCapability{}).Where("channel_id IN ?", []int{1, 4}).Update("channel_type", snapshotType).Error)
			request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, base+"/v1/responses", strings.NewReader(`{"model":"gpt-4o-mini","input":"retired-probe"}`))
			require.NoError(t, err)
			request.Header.Set("Authorization", "Bearer "+key)
			request.Header.Set("Content-Type", "application/json")
			response, err := http.DefaultClient.Do(request)
			require.NoError(t, err)
			defer response.Body.Close()
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode, string(body))
			assert.Zero(t, sends.Load(), "retired credentials must never reach a transport")
			for _, table := range []any{&model.BillingRecovery{}, &model.BillingEvent{}} {
				var count int64
				require.NoError(t, model.DB.Model(table).Count(&count).Error)
				assert.Zero(t, count, "retirement must be enforced before funds reservation")
			}
		})
	}
}
