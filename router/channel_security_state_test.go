package router

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func channelSecurityFixture(t *testing.T) (*gin.Engine, *gorm.DB, string, string, model.Channel) {
	t.Helper()
	initializeChannelSecurityColumns(t)
	engine, db := setupDashboardSystemStatusRouter(t)
	require.NoError(t, db.AutoMigrate(&model.Channel{}, &model.Ability{}, &model.SystemTask{}, &model.Model{}, &model.Vendor{}, &model.CasbinRule{}, &model.AuthzRole{}))
	oldCache, oldMaster := common.MemoryCacheEnabled, common.IsMasterNode
	common.MemoryCacheEnabled = false
	common.IsMasterNode = true
	require.NoError(t, authz.Init(db))
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache; common.IsMasterNode = oldMaster })
	root, session := createSystemStatusSession(t, db, 91801, "channel-security-root", common.RoleRootUser)
	makePAT := func(raw string, scopes []string) string {
		token := model.UserAccessToken{UserId: root.Id, Name: raw, TokenHash: model.AccessTokenFingerprint(raw), ExpiresAt: time.Now().Add(time.Hour).Unix(), CreatedAt: time.Now().Unix()}
		require.NoError(t, token.SetScopes(scopes))
		require.NoError(t, db.Create(&token).Error)
		return raw
	}
	read := makePAT("nap_channel_security_read", []string{"channel:read"})
	makePAT("nap_channel_security_write", []string{"channel:read", "channel:write", "channel:operate"})
	makePAT("nap_channel_security_sensitive", []string{"channel:read", "channel:write", "channel:sensitive_write"})
	header := `{"Authorization":"Bearer synthetic-header-secret"}`
	setting := `{"proxy":"http://test-user:synthetic-proxy-secret@127.0.0.1:9000"}`
	channel := model.Channel{Type: 1, Name: "channel-security", Key: "synthetic-key", Status: common.ChannelStatusEnabled, Models: "test-model", ChannelRatio: "1", HeaderOverride: &header, Setting: &setting, OtherSettings: `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/v1/chat/completions","auth":{"type":"header","name":"Authorization","value":"synthetic-route-secret"}}]}}`}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(db))
	return engine, db, session, read, channel
}

func channelSecurityRequest(t *testing.T, engine *gin.Engine, bearer, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, req)
	return response
}

func TestChannelReadRedactsCredentialFieldsByEffectivePermission(t *testing.T) {
	engine, db, root, read, channel := channelSecurityFixture(t)
	_, admin := createSystemStatusSession(t, db, 91802, "channel-security-admin", common.RoleAdminUser)
	for _, bearer := range []string{read, admin} {
		for _, path := range []string{fmt.Sprintf("/api/channel/%d", channel.Id), "/api/channel/", "/api/channel/search?keyword=channel-security"} {
			response := channelSecurityRequest(t, engine, bearer, http.MethodGet, path, "")
			require.Equal(t, 200, response.Code, response.Body.String())
			for _, secret := range []string{"synthetic-header-secret", "synthetic-proxy-secret", "synthetic-route-secret"} {
				require.NotContains(t, response.Body.String(), secret)
			}
			require.Contains(t, response.Body.String(), "redacted_fields")
			require.Contains(t, response.Body.String(), "__ren2hub_redacted__")
			if bearer == read && path == fmt.Sprintf("/api/channel/%d", channel.Id) {
				channelSecurityContract(t, "redacted_channel", response)
			}
		}
	}
	for _, bearer := range []string{root, "nap_channel_security_write"} {
		response := channelSecurityRequest(t, engine, bearer, http.MethodGet, fmt.Sprintf("/api/channel/%d", channel.Id), "")
		require.Equal(t, 200, response.Code)
		require.Contains(t, response.Body.String(), "synthetic-header-secret")
	}
}

func TestChannelWritesPreserveRedactionsAndIntersectPATScopes(t *testing.T) {
	engine, db, _, _, channel := channelSecurityFixture(t)
	write := "nap_channel_security_write"
	body := fmt.Sprintf(`{"id":%d,"type":1,"name":"edited","models":"test-model","header_override":"{\"__ren2hub_redacted__\":true}","setting":"{\"__ren2hub_redacted__\":true}","settings":"{\"__ren2hub_redacted__\":true}"}`, channel.Id)
	// An administrator without visibility can echo complete redacted fields.
	_, admin := createSystemStatusSession(t, db, 91803, "channel-redaction-admin", common.RoleAdminUser)
	adminPAT := model.UserAccessToken{UserId: 91803, Name: "admin-scope", TokenHash: model.AccessTokenFingerprint("nap_admin_sensitive"), ExpiresAt: time.Now().Add(time.Hour).Unix()}
	require.NoError(t, adminPAT.SetScopes([]string{"channel:write", "channel:sensitive_write"}))
	require.NoError(t, db.Create(&adminPAT).Error)
	denied := channelSecurityRequest(t, engine, "nap_admin_sensitive", http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"header_override":"{\"Authorization\":\"denied\"}"}`, channel.Id))
	require.NotContains(t, denied.Body.String(), `"success":true`)
	response := channelSecurityRequest(t, engine, admin, http.MethodPut, "/api/channel/", body)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"success":true`)
	var saved model.Channel
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.Equal(t, channel.HeaderOverride, saved.HeaderOverride)
	require.Equal(t, channel.Setting, saved.Setting)
	require.Equal(t, channel.OtherSettings, saved.OtherSettings)
	// Root's write-only PAT must not inherit the account's sensitive_write grant.
	response = channelSecurityRequest(t, engine, write, http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"type":1,"name":"edited","models":"test-model","header_override":"{\"Authorization\":\"replacement\"}"}`, channel.Id))
	require.NotContains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.Equal(t, channel.HeaderOverride, saved.HeaderOverride)
	response = channelSecurityRequest(t, engine, admin, http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"type":1,"name":"edited","models":"test-model","header_override":"{\"__ren2hub_redacted__\":true,\"X-Extra\":\"forged\"}"}`, channel.Id))
	require.Equal(t, 400, response.Code)
	require.Contains(t, response.Body.String(), "CHANNEL_REDACTED_FIELD_INVALID")
	channelSecurityContract(t, "redacted_field_invalid", response)
	response = channelSecurityRequest(t, engine, "nap_channel_security_sensitive", http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"header_override":"{\"Authorization\":\"allowed-replacement\"}"}`, channel.Id))
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.Equal(t, `{"Authorization":"allowed-replacement"}`, *saved.HeaderOverride)
	// Clearing is an ordinary sensitive field change: account and PAT must
	// both grant sensitive_write. Omission continues to preserve the value.
	for _, bearer := range []string{admin, write} {
		response = channelSecurityRequest(t, engine, bearer, http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"header_override":""}`, channel.Id))
		require.NotContains(t, response.Body.String(), `"success":true`)
		require.NoError(t, db.First(&saved, channel.Id).Error)
		require.Equal(t, `{"Authorization":"allowed-replacement"}`, *saved.HeaderOverride)
	}
	response = channelSecurityRequest(t, engine, "nap_channel_security_sensitive", http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"header_override":""}`, channel.Id))
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.NotNil(t, saved.HeaderOverride)
	require.Empty(t, *saved.HeaderOverride)
	require.NoError(t, db.Model(&saved).Update("header_override", channel.HeaderOverride).Error)
	// An omitted field also preserves credentials; a fake marker on a visible
	// field, including an escaped marker key, is rejected rather than persisted.
	response = channelSecurityRequest(t, engine, admin, http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"name":"omitted"}`, channel.Id))
	require.Contains(t, response.Body.String(), `"success":true`)
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.Equal(t, channel.HeaderOverride, saved.HeaderOverride)
	for _, marker := range []string{`{"__ren2hub_redacted__":true}`, `{"\u005f_ren2hub_redacted__":true}`} {
		payload, err := json.Marshal(map[string]any{"id": channel.Id, "header_override": marker})
		require.NoError(t, err)
		response = channelSecurityRequest(t, engine, write, http.MethodPut, "/api/channel/", string(payload))
		require.Equal(t, 400, response.Code, response.Body.String())
	}
}

func TestChannelStatusRoutesReportStorageFailureAndRecover(t *testing.T) {
	engine, db, root, _, channel := channelSecurityFixture(t)
	common.MemoryCacheEnabled = true
	model.InitChannelCache()
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("channel_status_storage_error", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Ability" {
			tx.AddError(errors.New("synthetic-private-database-error"))
		}
	}))
	t.Cleanup(func() { db.Callback().Update().Remove("channel_status_storage_error") })
	response := channelSecurityRequest(t, engine, root, http.MethodPost, fmt.Sprintf("/api/channel/%d/status", channel.Id), `{"status":2}`)
	require.Equal(t, 500, response.Code)
	require.Contains(t, response.Body.String(), "CHANNEL_STATUS_UPDATE_FAILED")
	require.NotContains(t, response.Body.String(), "synthetic-private-database-error")
	channelSecurityContract(t, "status_storage_failure", response)
	cached, err := model.CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, cached.Status)
	response = channelSecurityRequest(t, engine, root, http.MethodPost, "/api/channel/status/batch", fmt.Sprintf(`{"ids":[%d,99999],"status":2}`, channel.Id))
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"success":false`)
	require.Contains(t, response.Body.String(), "failed_ids")
	channelSecurityContract(t, "status_batch_failure", response)
	require.NoError(t, db.Callback().Update().Remove("channel_status_storage_error"))
	response = channelSecurityRequest(t, engine, root, http.MethodPost, fmt.Sprintf("/api/channel/%d/status", channel.Id), `{"status":2}`)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"data":true`)
	channelSecurityContract(t, "status_changed", response)
	response = channelSecurityRequest(t, engine, root, http.MethodPost, fmt.Sprintf("/api/channel/%d/status", channel.Id), `{"status":2}`)
	require.Contains(t, response.Body.String(), `"data":false`)
	channelSecurityContract(t, "status_unchanged", response)
	response = channelSecurityRequest(t, engine, root, http.MethodPost, "/api/channel/99999/status", `{"status":2}`)
	require.Equal(t, 404, response.Code)
	require.Contains(t, response.Body.String(), "CHANNEL_NOT_FOUND")
	channelSecurityContract(t, "status_missing", response)
	oldPublish := model.ChannelRoutingChanged
	model.ChannelRoutingChanged = func(int) error { return errors.New("synthetic-publication-error") }
	t.Cleanup(func() { model.ChannelRoutingChanged = oldPublish })
	response = channelSecurityRequest(t, engine, root, http.MethodPost, fmt.Sprintf("/api/channel/%d/status", channel.Id), `{"status":1}`)
	require.Equal(t, 200, response.Code)
	require.Contains(t, response.Body.String(), `"publication_pending":true`)
	channelSecurityContract(t, "status_publication_pending", response)
	response = channelSecurityRequest(t, engine, root, http.MethodPost, "/api/channel/status/batch", fmt.Sprintf(`{"ids":[%d,99999],"status":2}`, channel.Id))
	require.Contains(t, response.Body.String(), `"data":1`)
	require.Contains(t, response.Body.String(), `"publication_pending":1`)
	require.NotContains(t, response.Body.String(), "synthetic-publication-error")
	channelSecurityContract(t, "status_batch_partial", response)
}

func initializeChannelSecurityColumns(t *testing.T) {
	t.Helper()
	oldDB, oldPath, oldMaster := model.DB, common.SQLitePath, common.IsMasterNode
	oldMain, oldLog := common.MainDatabaseType(), common.LogDatabaseType()
	defer func() {
		model.DB = oldDB
		common.SQLitePath = oldPath
		common.IsMasterNode = oldMaster
		common.SetDatabaseTypes(oldMain, oldLog)
	}()
	t.Setenv("SQL_DSN", "local")
	common.SQLitePath = filepath.Join(t.TempDir(), "columns.db")
	common.IsMasterNode = false
	require.NoError(t, model.InitDB())
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

var updateChannelSecurityContract = flag.Bool("update", false, "rewrite channel security frontend contract fixture")

// These fixed responses come from registered handlers and are consumed by
// frontend contract tests. -update writes only this named contract fixture.
func channelSecurityContract(t *testing.T, name string, response *httptest.ResponseRecorder) {
	t.Helper()
	path := "../frontend/tests/fixtures/native/channel-security.json"
	fixture := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	if err == nil {
		require.NoError(t, json.Unmarshal(data, &fixture))
	} else {
		require.True(t, os.IsNotExist(err))
	}
	actual, err := json.Marshal(map[string]any{"status": response.Code, "body": json.RawMessage(response.Body.Bytes())})
	require.NoError(t, err)
	if *updateChannelSecurityContract {
		fixture[name] = actual
		data, err = json.MarshalIndent(fixture, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, append(data, '\n'), 0644))
	}
	require.Contains(t, fixture, name, "missing frontend contract fixture")
	require.JSONEq(t, string(fixture[name]), string(actual))
}
