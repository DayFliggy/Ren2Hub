package router

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/stretchr/testify/require"
)

func TestUpstreamRoutesAuthzPermissionsAndRetiredGroups(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccount{}, &model.UpstreamChannelBinding{}, &model.CasbinRule{}, &model.AuthzRole{}, &model.Channel{}))
	require.NoError(t, authz.Init(db))
	root, rootSession := createSystemStatusSession(t, db, 92101, "up-root", common.RoleRootUser)
	_, admin := createSystemStatusSession(t, db, 92102, "up-admin", common.RoleAdminUser)
	_, user := createSystemStatusSession(t, db, 92103, "up-user", common.RoleCommonUser)
	patRaw := "nap_upstream-test-only"
	pat := model.UserAccessToken{UserId: root.Id, Name: "upstream editor", TokenHash: model.AccessTokenFingerprint(patRaw), ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, pat.SetScopes([]string{"upstream:write", "upstream:sensitive_write"}))
	require.NoError(t, db.Create(&pat).Error)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); http.Error(w, "unexpected request", 500) }))
	defer server.Close()
	channelPAT := "nap_upstream-test-channel-only"
	token := model.UserAccessToken{UserId: root.Id, Name: "channel only", TokenHash: model.AccessTokenFingerprint(channelPAT), ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, token.SetScopes([]string{"channel:sensitive_write"}))
	require.NoError(t, db.Create(&token).Error)
	custom, session := createSystemStatusSession(t, db, 92104, "up-custom", common.RoleAdminUser)
	require.NoError(t, authz.SetUserPermissions(custom.Id, authz.PermissionsMap{"upstream": {"write": false, "sensitive_write": false}, "channel": {"sensitive_write": true}}))
	require.True(t, authz.Can(custom.Id, custom.Role, authz.ChannelSensitiveWrite))
	a := model.UpstreamAccount{Name: "contract", Platform: "newapi", BaseURL: server.URL, CredentialCiphertext: "private-ciphertext", Status: "active"}
	require.NoError(t, model.CreateUpstreamAccount(&a))
	call := func(method, path, token, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/admin/upstreams"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}
	for _, tc := range []struct {
		method, path, bearer, body string
		status                     int
	}{
		{"GET", "", "", "", 401}, {"GET", "", user, "", 403}, {"GET", "?upstream_group_id=1", admin, "", 200}, {"GET", "?group=1", rootSession, "", 400}, {"GET", "", patRaw, "", 403},
		{"PATCH", "/1/endpoint", admin, `{"base_url":"https://new.invalid"}`, 403},
		{"PATCH", "/1", admin, `{"base_url":"https://new.invalid"}`, 400},
		{"POST", "/1/keys", admin, `{"name":"x"}`, 403},
		{"POST", "/1/keys/7/bind", patRaw, `{"channel_id":8}`, 403}, {"POST", "/1/keys/7/channel", patRaw, `{"name":"x"}`, 403},
		{"POST", "/1/channels", patRaw, `{"name":"x","new_key":{"name":"x"}}`, 403}, {"PATCH", "/1/bindings/8", patRaw, `{"ratio_sync_enabled":true}`, 403}, {"POST", "/1/bindings/8/reverify", patRaw, "", 403}, {"DELETE", "/1/bindings/8", patRaw, "", 403},
	} {
		w := call(tc.method, tc.path, tc.bearer, tc.body)
		require.Equal(t, tc.status, w.Code, "%s %s %s", tc.method, tc.path, w.Body.String())
	}
	ch := model.Channel{Key: "test-secret", Name: "original", Models: "m", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&ch).Error)
	b := model.UpstreamChannelBinding{AccountID: a.ID, RemoteKeyID: 7, ChannelID: ch.Id, ChannelFP: "fixture", RemoteStatus: "active"}
	require.NoError(t, db.Create(&b).Error)
	for _, bearer := range []string{patRaw, channelPAT, session} {
		for _, op := range []struct{ method, path, body string }{
			{"POST", "/1/keys/7/bind", fmt.Sprintf(`{"channel_id":%d}`, ch.Id)},
			{"POST", "/1/keys/7/channel", `{"name":"new","models":"m"}`},
			{"POST", "/1/channels", `{"name":"new","models":"m","new_key":{"name":"new"}}`},
			{"PATCH", fmt.Sprintf("/1/bindings/%d", ch.Id), `{"ratio_sync_enabled":true}`},
			{"POST", fmt.Sprintf("/1/bindings/%d/reverify", ch.Id), ""},
			{"DELETE", fmt.Sprintf("/1/bindings/%d", ch.Id), ""},
		} {
			resp := call(op.method, op.path, bearer, op.body)
			require.Equal(t, 403, resp.Code)
			var channels []model.Channel
			require.NoError(t, db.Find(&channels).Error)
			require.Len(t, channels, 1)
			require.Equal(t, ch.Key, channels[0].Key)
			require.Equal(t, ch.Status, channels[0].Status)
			rows, e := model.ListUpstreamBindings(a.ID)
			require.NoError(t, e)
			require.Equal(t, []model.UpstreamChannelBinding{b}, rows)
			require.Zero(t, hits.Load())
		}
	}
	before, err := model.GetUpstreamAccount(a.ID)
	require.NoError(t, err)
	require.Equal(t, a.CredentialCiphertext, before.CredentialCiphertext)
	require.Equal(t, a.BaseURL, before.BaseURL)
	w := call("PATCH", "/1/endpoint", rootSession, `{"proxy_url":"http://proxy.invalid:8080"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	changed, err := model.GetUpstreamAccount(a.ID)
	require.NoError(t, err)
	require.Empty(t, changed.CredentialCiphertext)
	require.Equal(t, "reauth_required", changed.Status)
	w = call("POST", "/auth", rootSession, `{"password":"private-upstream-password","bad":true}`)
	require.Equal(t, 400, w.Code)
	var audit []model.AuditLog
	require.NoError(t, db.Find(&audit).Error)
	require.NotEmpty(t, audit)
	encoded, err := common.Marshal(audit)
	require.NoError(t, err)
	for _, s := range []string{patRaw, "private-upstream-password", "private-ciphertext"} {
		require.NotContains(t, string(encoded), s)
		require.NotContains(t, w.Body.String(), s)
	}
}

func TestUpstreamTwoAccountsContendForChannelThroughRouter(t *testing.T) {
	engine, db := setupDashboardSystemStatusRouter(t)
	original := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = original })
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccount{}, &model.UpstreamChannelBinding{}, &model.Channel{}, &model.CasbinRule{}, &model.AuthzRole{}))
	require.NoError(t, authz.Init(db))
	_, token := createSystemStatusSession(t, db, 92111, "up-contend", common.RoleRootUser)
	t.Setenv("CRYPTO_SECRET", "fixture")
	protection := system_setting.GetFetchSetting()
	enabled := protection.EnableSSRFProtection
	protection.EnableSSRFProtection = false
	t.Cleanup(func() { protection.EnableSSRFProtection = enabled })
	var fetching atomic.Int32
	both := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/refresh":
			fmt.Fprint(w, `{"code":0,"data":{"access_token":"fixture-access","refresh_token":"fixture-refresh","expires_in":86400}}`)
		case "/api/v1/keys/5":
			if fetching.Add(1) == 2 {
				close(both)
			}
			select {
			case <-both:
			case <-r.Context().Done():
				return
			}
			fmt.Fprint(w, `{"code":0,"data":{"id":5,"name":"key","key":"fixture-key","status":"active"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	accounts := []int{}
	for range 2 {
		out, err := service.StartUpstreamAuth(context.Background(), 1, service.UpstreamAuthRequest{Name: "remote", Platform: "sub2api", BaseURL: server.URL, RefreshToken: "fixture"})
		require.NoError(t, err)
		accounts = append(accounts, out.AccountID)
	}
	ch := model.Channel{Name: "channel", Key: "fixture-key", CreatedTime: 422, Type: 59, BaseURL: &server.URL}
	require.NoError(t, db.Create(&ch).Error)
	results := make(chan *httptest.ResponseRecorder, 2)
	for _, id := range accounts {
		go func() {
			req := httptest.NewRequest("POST", fmt.Sprintf("/api/admin/upstreams/%d/keys/5/bind", id), strings.NewReader(fmt.Sprintf(`{"channel_id":%d}`, ch.Id)))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			engine.ServeHTTP(w, req)
			results <- w
		}()
	}
	statuses := []int{}
	for range 2 {
		w := <-results
		statuses = append(statuses, w.Code)
		if w.Code == 409 {
			require.Contains(t, w.Body.String(), `"code":"remote_key_already_bound"`)
		}
	}
	require.ElementsMatch(t, []int{200, 409}, statuses)
	rows, err := model.ListUpstreamBindingsForChannels([]int{ch.Id})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestChannelUpstreamAccountServerFilterAndAuthz(t *testing.T) {
	// Ensure model dialect column names (commonKeyCol) are initialized for SearchChannels.
	prevSQLitePath := common.SQLitePath
	prevMainType, prevLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SQLitePath = fmt.Sprintf("file:%s_colinit?mode=memory&cache=shared", t.Name())
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Setenv("SQL_DSN", "")
	require.NoError(t, model.InitDB())
	if model.DB != nil {
		if sqlDB, err := model.DB.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	common.SQLitePath = prevSQLitePath
	common.SetDatabaseTypes(prevMainType, prevLogType)

	engine, db := setupDashboardSystemStatusRouter(t)
	master := common.IsMasterNode
	common.IsMasterNode = true
	t.Cleanup(func() { common.IsMasterNode = master })
	require.NoError(t, db.AutoMigrate(&model.UpstreamAccount{}, &model.UpstreamChannelBinding{}, &model.CasbinRule{}, &model.AuthzRole{}, &model.Channel{}, &model.Model{}, &model.Vendor{}))
	require.NoError(t, authz.Init(db))

	_, rootSession := createSystemStatusSession(t, db, 92301, "ch-filter-root", common.RoleRootUser)
	noUpstreamAdmin, noUpstreamSession := createSystemStatusSession(t, db, 92302, "ch-filter-no-up", common.RoleAdminUser)
	require.NoError(t, authz.SetUserPermissions(noUpstreamAdmin.Id, authz.PermissionsMap{
		"channel":  {"read": true},
		"upstream": {"read": false},
	}))

	a1 := model.UpstreamAccount{Name: "acct-1", Platform: "newapi", BaseURL: "https://one.invalid", Status: "active"}
	a2 := model.UpstreamAccount{Name: "acct-2", Platform: "sub2api", BaseURL: "https://two.invalid", Status: "active"}
	require.NoError(t, model.CreateUpstreamAccount(&a1))
	require.NoError(t, model.CreateUpstreamAccount(&a2))

	c1 := model.Channel{Name: "alpha-bound-1", Key: "k1", Models: "gpt-4o", Type: 60, Status: common.ChannelStatusEnabled, CreatedTime: 1001}
	c2 := model.Channel{Name: "alpha-bound-2", Key: "k2", Models: "gpt-4o", Type: 59, Status: common.ChannelStatusEnabled, CreatedTime: 1002}
	c3 := model.Channel{Name: "alpha-unbound", Key: "k3", Models: "gpt-4o", Type: 1, Status: common.ChannelStatusEnabled, CreatedTime: 1003}
	require.NoError(t, db.Create(&c1).Error)
	require.NoError(t, db.Create(&c2).Error)
	require.NoError(t, db.Create(&c3).Error)

	require.NoError(t, db.Create(&model.UpstreamChannelBinding{
		AccountID: a1.ID, RemoteKeyID: 10, ChannelID: c1.Id, ChannelCreatedTime: c1.CreatedTime, ChannelFP: "fp1", RemoteStatus: "active",
	}).Error)
	require.NoError(t, db.Create(&model.UpstreamChannelBinding{
		AccountID: a2.ID, RemoteKeyID: 20, ChannelID: c2.Id, ChannelCreatedTime: c2.CreatedTime, ChannelFP: "fp2", RemoteStatus: "active",
	}).Error)

	call := func(path, bearer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, req)
		return w
	}

	// 1. Missing upstream:read permission gets 403 on both list and search when filtering by upstream_account.
	require.Equal(t, http.StatusForbidden, call(fmt.Sprintf("/api/channel/?upstream_account=%d", a1.ID), noUpstreamSession).Code)
	require.Equal(t, http.StatusForbidden, call(fmt.Sprintf("/api/channel/search?keyword=alpha&upstream_account=%d", a1.ID), noUpstreamSession).Code)
	// Same admin can still list channels normally without upstream_account.
	require.Equal(t, http.StatusOK, call("/api/channel/", noUpstreamSession).Code)

	// 2. Invalid upstream_account returns 400.
	require.Equal(t, http.StatusBadRequest, call("/api/channel/?upstream_account=abc", rootSession).Code)
	require.Equal(t, http.StatusBadRequest, call("/api/channel/search?keyword=alpha&upstream_account=0", rootSession).Code)

	// 3. Valid upstream_account filters list & search accurately on the server side.
	wList := call(fmt.Sprintf("/api/channel/?upstream_account=%d", a1.ID), rootSession)
	require.Equal(t, http.StatusOK, wList.Code)
	require.Contains(t, wList.Body.String(), "alpha-bound-1")
	require.NotContains(t, wList.Body.String(), "alpha-bound-2")
	require.NotContains(t, wList.Body.String(), "alpha-unbound")
	require.Contains(t, wList.Body.String(), `"total":1`)

	wSearch := call(fmt.Sprintf("/api/channel/search?keyword=alpha&upstream_account=%d", a2.ID), rootSession)
	require.Equal(t, http.StatusOK, wSearch.Code)
	require.Contains(t, wSearch.Body.String(), "alpha-bound-2")
	require.NotContains(t, wSearch.Body.String(), "alpha-bound-1")
	require.NotContains(t, wSearch.Body.String(), "alpha-unbound")
	require.Contains(t, wSearch.Body.String(), `"total":1`)
}
