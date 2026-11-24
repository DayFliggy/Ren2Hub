package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestFrontendContractUpstreams(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	require.NoError(t, model.DB.AutoMigrate(&model.UpstreamAccount{}, &model.UpstreamChannelBinding{}))
	a := model.UpstreamAccount{ID: 4, Name: "演示账号", Platform: "newapi", BaseURL: "https://example.invalid", ProxyURL: "http://user:password@proxy.invalid", Status: "active", CredentialCiphertext: "private-fixture-ciphertext", GroupsJSON: `[{"name":"standard","ratio":"1.25"}]`, SiteMetaJSON: `{"username":"demo","balance":"300","upstream_group":"standard"}`, NextSyncAt: 1800000120, LastSyncAt: 1800000000}
	require.NoError(t, model.CreateUpstreamAccount(&a))
	b := model.UpstreamChannelBinding{ID: 1, AccountID: a.ID, ChannelID: 9, RemoteKeyID: 14, ChannelCreatedTime: 1799999900, ChannelFP: "private-fingerprint", RemoteStatus: "active", RatioSyncFactor: "1.25"}
	require.NoError(t, model.DB.Create(&b).Error)
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET("/api/admin/upstreams", ListManagedUpstreams)
	engine.GET("/api/admin/upstreams/:id", GetManagedUpstream)
	engine.GET("/api/admin/upstreams/:id/bindings", ListManagedBindings)
	fixture := gin.H{}
	for _, tc := range []struct{ name, path string }{{"list", ""}, {"detail", "/4"}, {"bindings", "/4/bindings"}} {
		w := contractRequest(t, engine, "GET", "/api/admin/upstreams"+tc.path, nil)
		require.Equal(t, 200, w.Code)
		body := decodeContractBody(t, w)
		require.Equal(t, true, body["success"])
		for _, s := range []string{"ciphertext", "fingerprint", "proxy.invalid", "credential_uid"} {
			require.NotContains(t, w.Body.String(), s)
		}
		fixture[tc.name] = body
	}
	addUpstreamEditorFixtures(t, fixture)
	frontendFixture(t, "upstreams.get.json", fixture)
}

// Exercise the editor contract against both remote protocols. List and update
// use the real account credential flow, so redaction and platform units are
// visible in the same envelopes consumed by the client.
func addUpstreamEditorFixtures(t *testing.T, fixture gin.H) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	prev := *fetch
	fetch.AllowPrivateIp = true
	fetch.AllowedPorts = []string{"1-65535"}
	t.Cleanup(func() { *fetch = prev })
	t.Setenv("CRYPTO_SECRET", "upstream-contract-test")
	const newKey = `{"id":1,"name":"limited","key":"synthetic-private-key-for-tests","status":1,"expired_time":-1,"remain_quota":9000,"used_quota":1000,"unlimited_quota":false,"group":"standard","allow_ips":"","model_limits":"","model_limits_enabled":false,"cross_group_retry":false}`
	const subKey = `{"id":1,"name":"metered","key":"synthetic-private-key-for-tests","status":"active","group_id":7,"quota":30.5,"quota_used":4.75,"expires_at":"2099-01-01T00:00:00Z"}`
	const prefix = "/api/admin/upstreams"
	var newUpdate, subUpdate map[string]any
	mux := http.NewServeMux()
	newAPI := map[string]string{
		"GET /api/status":           `{"version":"1.0.0-rc.51","password_login_enabled":true,"turnstile_check":true,"system_name":"test-newapi","password_login_encryption_enabled":true,"quota_display_type":"CNY","quota_per_unit":1000,"usd_exchange_rate":7,"secret":"synthetic-private-key-for-tests"}`,
		"GET /api/user/self":        `{"username":"editor","quota":9000,"group":"standard"}`,
		"GET /api/user/self/groups": `{"standard":{"ratio":1}}`,
		"GET /api/token/":           `{"page":1,"page_size":100,"total":1,"items":[` + newKey + `]}`,
		"GET /api/token/1":          newKey,
	}
	for route, data := range newAPI {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"success":true,"data":%s}`, data) })
	}
	sub2API := map[string]string{
		"GET /api/v1/settings/public": `{"site_name":"test","version":"0.2.25","turnstile_enabled":false,"totp_enabled":true,"backend_mode_enabled":false}`,
		"POST /api/v1/auth/refresh":   `{"access_token":"test-access","refresh_token":"test-refresh","expires_in":86400}`,
		"GET /api/v1/keys":            `{"page":1,"page_size":200,"pages":1,"total":1,"items":[` + subKey + `]}`,
	}
	for route, data := range sub2API {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) { fmt.Fprintf(w, `{"code":0,"data":%s}`, data) })
	}
	mux.HandleFunc("PUT /api/token/", func(w http.ResponseWriter, r *http.Request) {
		if err := common.DecodeJsonBody(r.Body, &newUpdate); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"success":true}`)
	})
	mux.HandleFunc("PUT /api/v1/keys/1", func(w http.ResponseWriter, r *http.Request) {
		if err := common.DecodeJsonBody(r.Body, &subUpdate); err != nil {
			t.Error(err)
		}
		fmt.Fprint(w, `{"code":0}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	engine := contractEngine(1, common.RoleRootUser)
	engine.GET(prefix+"/:id", GetManagedUpstream)
	engine.POST(prefix+"/probe", ProbeUpstream)
	engine.POST(prefix+"/auth", StartUpstreamAuth)
	engine.GET(prefix+"/:id/keys", ListManagedKeys)
	engine.PATCH(prefix+"/:id/keys/:key_id", EditManagedKey)
	engine.POST(prefix+"/:id/sync", SyncManagedUpstream)
	call := func(method, path string, body any) map[string]any {
		var raw []byte
		if body != nil {
			var err error
			raw, err = common.Marshal(body)
			require.NoError(t, err)
		}
		w := contractRequest(t, engine, method, prefix+path, raw)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "synthetic-private-key-for-tests")
		require.NotContains(t, strings.ToLower(w.Body.String()), "sha256")
		return decodeContractBody(t, w)
	}
	for _, platform := range []string{"newapi", "sub2api"} {
		auth := gin.H{"platform": platform, "base_url": server.URL}
		probe := call("POST", "/probe", auth)
		fixture[platform+"_probe"] = probe
		caps := probe["data"].(map[string]any)
		require.Equal(t, platform == "sub2api", caps["password_login_available"])
		auth["name"] = "editor"
		if platform == "newapi" {
			auth["pat"] = "test-pat"
		} else {
			auth["refresh_token"] = "test-refresh"
		}
		data := call("POST", "/auth", auth)["data"].(map[string]any)
		path := "/" + strconv.Itoa(int(data["account_id"].(float64)))
		if platform == "newapi" {
			call("POST", path+"/sync", nil)
			meta := call("GET", path, nil)["data"].(map[string]any)["site_meta"].(map[string]any)
			require.Equal(t, float64(1000), meta["quota_per_unit"])
			require.Equal(t, "9000", meta["balance"])
			fixture["newapi_site_meta"] = meta
		}
		keys := call("GET", path+"/keys", nil)
		fixture[platform+"_keys"] = keys
		key := keys["data"].([]any)[0].(map[string]any)
		input := gin.H{"name": "renamed"}
		if platform == "newapi" {
			for _, name := range []string{"expired_time", "remain_quota", "unlimited_quota"} {
				input[name] = key[name]
			}
			call("PATCH", path+"/keys/1", input)
			require.Equal(t, float64(9000), newUpdate["remain_quota"])
			require.Equal(t, false, newUpdate["unlimited_quota"])
			require.Equal(t, float64(-1), newUpdate["expired_time"])
		} else {
			input["quota"], input["expires_at"] = key["quota"], key["expires_at"]
			call("PATCH", path+"/keys/1", input)
			require.Equal(t, 30.5, subUpdate["quota"])
			require.Equal(t, "2099-01-01T00:00:00Z", subUpdate["expires_at"])
			require.Equal(t, 4.75, key["quota_used"])
		}
	}
}

func TestUpstreamControllerAuditRedactionAndDualPermissions(t *testing.T) {
	setupTaskPluginBindChannelTest(t)
	engine := contractEngine(4, common.RoleAdminUser)
	ran := false
	engine.PATCH("/binding", RequireUpstreamChannelWrite, func(c *gin.Context) { ran = true })
	res := contractRequest(t, engine, "PATCH", "/binding", []byte(`{"password":"private-test-value"}`))
	require.Equal(t, 403, res.Code)
	require.False(t, ran)
	require.NotContains(t, res.Body.String(), "private-test-value")
	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	writeUpstreamResult(ctx, nil, errors.New("private-test-value"))
	require.Equal(t, 500, w.Code)
	require.NotContains(t, w.Body.String(), "private-test-value")
}
