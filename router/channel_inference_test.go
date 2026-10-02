package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferenceChannelStatusRequiresAdminReadAndFixedTarget(t *testing.T) {
	setupRelayRouterTestDB(t)
	require.NoError(t, model.DB.AutoMigrate(&model.AuditLog{}, &model.CasbinRule{}, &model.AuthzRole{}))
	wasMaster := common.IsMasterNode
	common.IsMasterNode = true
	require.NoError(t, authz.Init(model.DB))
	common.IsMasterNode = wasMaster
	old := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = old })
	service.InitHttpClient()
	var sends atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Empty(t, r.URL.RawQuery)
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/version", "/server_info":
			_, _ = w.Write([]byte(`{"version":"test-version","api_key":"must-not-leak"}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case "/metrics":
			_, _ = w.Write([]byte("process_start_time_seconds 1\n"))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	access := map[int]string{}
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser} {
		name := fmt.Sprintf("inference-%d", role)
		require.NoError(t, model.DB.Create(&model.User{Id: role, Username: name, AffCode: name, Role: role, Status: common.UserStatusEnabled}).Error)
		access[role] = createRouterBrowserSession(t, role)
	}
	engine := gin.New()
	registerChannelRoutes(engine.Group("/api"))
	for _, kind := range []struct {
		kind     int
		provider string
	}{{constant.ChannelTypeVLLM, "vllm"}, {constant.ChannelTypeSGLang, "sglang"}} {
		channel := model.Channel{Type: kind.kind, BaseURL: &upstream.URL, Key: "EMPTY", Name: kind.provider}
		require.NoError(t, model.DB.Create(&channel).Error)
		path := fmt.Sprintf("/api/channel/%d/%s/status?url=http://untrusted.invalid&key=client-secret", channel.Id, kind.provider)
		call := func(role int) *httptest.ResponseRecorder {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			if role != 0 {
				req.Header.Set("Authorization", "Bearer "+access[role])
			}
			engine.ServeHTTP(recorder, req)
			return recorder
		}
		before := sends.Load()
		assert.Equal(t, http.StatusUnauthorized, call(0).Code)
		assert.Equal(t, http.StatusForbidden, call(common.RoleCommonUser).Code)
		assert.Equal(t, before, sends.Load())
		response := call(common.RoleAdminUser)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		assert.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		assert.Contains(t, response.Body.String(), "test-version")
		assert.NotContains(t, response.Body.String(), "must-not-leak")
		assert.NotContains(t, response.Body.String(), "client-secret")
		assert.Equal(t, before+4, sends.Load())
		require.NoError(t, authz.SetUserPermissions(common.RoleAdminUser, authz.PermissionsMap{authz.ResourceChannel: {authz.ActionRead: false}}))
		assert.Equal(t, http.StatusForbidden, call(common.RoleAdminUser).Code)
		assert.Equal(t, before+4, sends.Load())
		require.NoError(t, authz.ClearUserPermissions(common.RoleAdminUser))
	}
}
