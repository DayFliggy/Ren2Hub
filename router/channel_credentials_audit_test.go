package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const auditChannelPlaceholder = `{"__ren2hub_redacted__":true}`

func TestChannelReadRedactsParamOverridesAndPluginConfig(t *testing.T) {
	engine, db, root, read, channel := channelSecurityFixture(t)
	param := `{"operations":[{"mode":"set_header","path":"Authorization","value":"synthetic-param-secret"}]}`
	require.NoError(t, db.Model(&channel).Update("param_override", param).Error)
	_, admin := createSystemStatusSession(t, db, 91804, "channel-audit-admin", common.RoleAdminUser)
	for _, bearer := range []string{read, admin} {
		for _, path := range []string{fmt.Sprintf("/api/channel/%d", channel.Id), "/api/channel/", "/api/channel/search?keyword=channel-security"} {
			response := channelSecurityRequest(t, engine, bearer, http.MethodGet, path, "")
			require.Equal(t, 200, response.Code)
			require.NotContains(t, response.Body.String(), "synthetic-param-secret")
			require.Contains(t, response.Body.String(), `"param_override"`)
			if bearer == read && path == fmt.Sprintf("/api/channel/%d", channel.Id) {
				channelSecurityContract(t, "redacted_param_override", response)
			}
		}
	}
	response := channelSecurityRequest(t, engine, root, http.MethodGet, fmt.Sprintf("/api/channel/%d", channel.Id), "")
	require.Contains(t, response.Body.String(), "synthetic-param-secret")
	for _, config := range []string{`{"api_password":"synthetic-plugin-secret"}`, `"synthetic-plugin-secret"`, `["synthetic-plugin-secret"]`, `0`, `false`} {
		require.NoError(t, db.Model(&channel).Update("settings", `{"plugin_config":`+config+`}`).Error)
		response = channelSecurityRequest(t, engine, read, http.MethodGet, fmt.Sprintf("/api/channel/%d", channel.Id), "")
		var body struct {
			Data struct {
				Settings string   `json:"settings"`
				Fields   []string `json:"redacted_fields"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, auditChannelPlaceholder, body.Data.Settings)
		require.Contains(t, body.Data.Fields, "settings")
		require.NotContains(t, response.Body.String(), "synthetic-plugin-secret")
		if config == `{"api_password":"synthetic-plugin-secret"}` {
			channelSecurityContract(t, "redacted_plugin_config", response)
		}
	}
	for _, config := range []string{`null`, `""`, `{}`, `[]`, `{"empty":[null,"",{}]}`} {
		settings := `{"plugin_config":` + config + `}`
		require.NoError(t, db.Model(&channel).Update("settings", settings).Error)
		response = channelSecurityRequest(t, engine, read, http.MethodGet, fmt.Sprintf("/api/channel/%d", channel.Id), "")
		var body struct {
			Data struct {
				Settings string   `json:"settings"`
				Fields   []string `json:"redacted_fields"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, settings, body.Data.Settings)
		require.NotContains(t, body.Data.Fields, "settings")
	}

}

func TestChannelPluginConfigReadAndOmittedWritePreserveCredentials(t *testing.T) {
	engine, db, _, read, channel := channelSecurityFixture(t)
	settings := `{"plugin_config":{"api_password":"synthetic-plugin-secret"}}`
	require.NoError(t, db.Model(&channel).Update("settings", settings).Error)
	response := channelSecurityRequest(t, engine, read, http.MethodGet, fmt.Sprintf("/api/channel/%d", channel.Id), "")
	require.NotContains(t, response.Body.String(), "synthetic-plugin-secret")
	_, admin := createSystemStatusSession(t, db, 91804, "channel-plugin-admin", common.RoleAdminUser)
	for _, fields := range []map[string]any{{"name": "plugin-omitted"}, {"name": "plugin-placeholder", "settings": auditChannelPlaceholder}} {
		fields["id"] = channel.Id
		payload, err := json.Marshal(fields)
		require.NoError(t, err)
		response = channelSecurityRequest(t, engine, admin, http.MethodPut, "/api/channel/", string(payload))
		require.Contains(t, response.Body.String(), `"success":true`)
		require.NotContains(t, response.Body.String(), "synthetic-plugin-secret")
		var saved model.Channel
		require.NoError(t, db.First(&saved, channel.Id).Error)
		require.Equal(t, settings, saved.OtherSettings)
	}
}

func TestChannelParamOverrideWritesPreserveReplaceAndClear(t *testing.T) {
	engine, db, root, _, channel := channelSecurityFixture(t)
	param := `{"Authorization":"synthetic-param-secret"}`
	require.NoError(t, db.Model(&channel).Update("param_override", param).Error)
	_, admin := createSystemStatusSession(t, db, 91804, "channel-audit-admin", common.RoleAdminUser)
	request := func(bearer string, fields map[string]any) string {
		fields["id"] = channel.Id
		payload, err := json.Marshal(fields)
		require.NoError(t, err)
		response := channelSecurityRequest(t, engine, bearer, http.MethodPut, "/api/channel/", string(payload))
		return response.Body.String()
	}
	for _, fields := range []map[string]any{{"name": "omitted-param"}, {"param_override": auditChannelPlaceholder, "name": "placeholder-param"}} {
		body := request(admin, fields)
		require.Contains(t, body, `"success":true`)
		require.NotContains(t, body, "synthetic-param-secret")
		var saved model.Channel
		require.NoError(t, db.First(&saved, channel.Id).Error)
		require.Equal(t, param, *saved.ParamOverride)
	}
	responseBody := request(root, map[string]any{"name": "stored-response"})
	var response struct {
		Data model.Channel `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(responseBody), &response))
	require.Equal(t, param, *response.Data.ParamOverride)
	require.Equal(t, channel.HeaderOverride, response.Data.HeaderOverride)
	require.Equal(t, channel.Setting, response.Data.Setting)
	require.Equal(t, channel.OtherSettings, response.Data.OtherSettings)
	require.Equal(t, channel.Type, response.Data.Type)
	require.Empty(t, response.Data.Key)

	for _, bearer := range []string{admin, "nap_channel_security_write"} {
		require.Contains(t, request(bearer, map[string]any{"param_override": ""}), `"success":false`)
	}
	for _, value := range []string{`{"Authorization":"replacement"}`, ""} {
		require.Contains(t, request("nap_channel_security_sensitive", map[string]any{"param_override": value}), `"success":true`)
		var saved model.Channel
		require.NoError(t, db.First(&saved, channel.Id).Error)
		require.Equal(t, value, *saved.ParamOverride)
	}
	require.Contains(t, request(root, map[string]any{"param_override": auditChannelPlaceholder}), "CHANNEL_REDACTED_FIELD_INVALID")
}

func TestChannelAddAndTagRejectRedactedFields(t *testing.T) {
	engine, db, root, _, channel := channelSecurityFixture(t)
	require.NoError(t, db.Model(&channel).Update("tag", "audit-tag").Error)
	for _, field := range []string{"header_override", "param_override", "setting", "settings"} {
		for _, marker := range []string{auditChannelPlaceholder, `{"nested":{"\u005f_ren2hub_redacted__":true},"mixed":"value"}`} {
			t.Run("add_"+field+"_"+marker, func(t *testing.T) {
				data := map[string]any{"mode": "single", "channel": map[string]any{"type": 1, "name": "forged-channel", "key": "key", "models": "test-model", field: marker}}
				payload, err := json.Marshal(data)
				require.NoError(t, err)
				response := channelSecurityRequest(t, engine, root, http.MethodPost, "/api/channel/", string(payload))
				require.Equal(t, 400, response.Code, response.Body.String())
				require.Contains(t, response.Body.String(), "CHANNEL_REDACTED_FIELD_INVALID")
				var count int64
				require.NoError(t, db.Model(&model.Channel{}).Where("name = ?", "forged-channel").Count(&count).Error)
				require.Zero(t, count)
			})
			if field == "header_override" || field == "param_override" {
				t.Run("tag_"+field+"_"+marker, func(t *testing.T) {
					payload, err := json.Marshal(map[string]any{"tag": "audit-tag", field: marker})
					require.NoError(t, err)
					response := channelSecurityRequest(t, engine, root, http.MethodPut, "/api/channel/tag", string(payload))
					require.Equal(t, 400, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), "CHANNEL_REDACTED_FIELD_INVALID")
					var saved model.Channel
					require.NoError(t, db.First(&saved, channel.Id).Error)
					require.Equal(t, channel.HeaderOverride, saved.HeaderOverride)
					require.Equal(t, channel.ParamOverride, saved.ParamOverride)
				})
			}
		}
	}
}

func TestChannelUpdateResponseReportsReadFailureAfterPersisting(t *testing.T) {
	engine, db, root, _, channel := channelSecurityFixture(t)
	oldPublish := model.ChannelRoutingChanged
	model.ChannelRoutingChanged = nil
	t.Cleanup(func() { model.ChannelRoutingChanged = oldPublish })
	written := false
	require.NoError(t, db.Callback().Update().After("gorm:update").Register("channel_audit_written", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" && tx.Error == nil {
			written = true
		}
	}))
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("channel_audit_read_failure", func(tx *gorm.DB) {
		if written && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "Channel" {
			tx.AddError(errors.New("synthetic-private-read-error"))
		}
	}))
	t.Cleanup(func() {
		db.Callback().Update().Remove("channel_audit_written")
		db.Callback().Query().Remove("channel_audit_read_failure")
	})
	response := channelSecurityRequest(t, engine, root, http.MethodPut, "/api/channel/", fmt.Sprintf(`{"id":%d,"name":"persisted-before-read-failure","models":"test-model"}`, channel.Id))
	require.Equal(t, 500, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), "CHANNEL_UPDATE_READ_FAILED")
	require.Contains(t, response.Body.String(), `"updated":true`)
	require.NotContains(t, response.Body.String(), "synthetic-private-read-error")
	channelSecurityContract(t, "update_read_failure", response)
	require.NoError(t, db.Callback().Query().Remove("channel_audit_read_failure"))
	var saved model.Channel
	require.NoError(t, db.First(&saved, channel.Id).Error)
	require.Equal(t, "persisted-before-read-failure", saved.Name)
}
