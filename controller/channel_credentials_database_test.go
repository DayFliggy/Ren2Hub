package controller

import (
	"fmt"
	"net/http"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/require"
)

// The route tests cover authorization. This matrix protects the durable patch
// semantics on every supported database through the real UpdateChannel handler.
func TestChannelCredentialPreservationDatabaseMatrix(t *testing.T) {
	for _, dialect := range []struct{ kind, env string }{{"sqlite", ""}, {"mysql", "TEST_MYSQL_DSN"}, {"postgres", "TEST_POSTGRES_DSN"}} {
		t.Run(dialect.kind, func(t *testing.T) {
			if dialect.env != "" && os.Getenv(dialect.env) == "" {
				t.Skip("set " + dialect.env + " to run this database")
			}
			db := modelManagementDB(t, dialect.kind, os.Getenv(dialect.env))
			require.NoError(t, db.AutoMigrate(&model.SystemTask{}))
			pluginKey := "credential-save-" + dialect.kind
			_, err := jsplugin.DefaultRegistry.Register(fmt.Sprintf(`
export const meta = {apiVersion: 1, key: %q, name: "Credential Save", version: "1.0.0", author: {name: "Test"}, models: ["doc"], fetchMode: "per_task"};
export function buildSubmitRequest() { return {}; }
export function parseSubmitResponse() { return {}; }
export function buildQueryRequest() { return {}; }
export function parseTaskResult() { return {}; }
`, pluginKey), jsplugin.Options{})
			require.NoError(t, err)
			t.Cleanup(func() { _ = jsplugin.DefaultRegistry.Unregister(pluginKey) })

			for _, scenario := range []struct {
				name string
				role int
			}{
				{"omitted_setting_during_sensitive_update", common.RoleRootUser},
				{"complete_placeholders_preserve_credentials", common.RoleAdminUser},
				{"explicit_empty_header_clears_override", common.RoleRootUser},
			} {
				t.Run(scenario.name, func(t *testing.T) {
					baseURL := "https://credential-save.example.test"
					setting := fmt.Sprintf(`{"task_plugin_key":%q,"proxy":"http://user:synthetic-secret@127.0.0.1:9000"}`, pluginKey)
					header := `{"Authorization":"Bearer synthetic-header"}`
					otherSettings := `{"advanced_custom":{"advanced_routes":[{"incoming_path":"/v1/chat/completions","upstream_path":"/chat","auth":{"type":"header","name":"Authorization","value":"synthetic-route"}}]}}`
					channel := model.Channel{
						Type: constant.ChannelTypeTaskPlugin, Name: scenario.name, Key: "synthetic-key", Models: "doc",
						BaseURL: &baseURL, Setting: &setting, HeaderOverride: &header, OtherSettings: otherSettings,
						ChannelRatio: "1", Status: common.ChannelStatusEnabled,
					}
					require.NoError(t, channel.Insert())
					payload := map[string]any{"id": channel.Id, "name": scenario.name + "-saved"}
					wantHeader := header
					switch scenario.name {
					case "omitted_setting_during_sensitive_update":
						payload["type"], payload["base_url"] = constant.ChannelTypeTaskPlugin, baseURL
						wantHeader = `{"Authorization":"Bearer replacement-header"}`
						payload["header_override"] = wantHeader
					case "complete_placeholders_preserve_credentials":
						payload["header_override"], payload["setting"], payload["settings"] = channelRedactedValue, channelRedactedValue, channelRedactedValue
					case "explicit_empty_header_clears_override":
						payload["header_override"] = ""
						wantHeader = ""
					}
					body, err := common.Marshal(payload)
					require.NoError(t, err)
					engine := contractEngine(1, scenario.role)
					engine.PUT("/api/channel/", UpdateChannel)
					response := contractRequest(t, engine, http.MethodPut, "/api/channel/", body)
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					require.Contains(t, response.Body.String(), `"success":true`, response.Body.String())

					var stored model.Channel
					require.NoError(t, db.First(&stored, channel.Id).Error)
					require.Equal(t, payload["name"], stored.Name)
					require.Equal(t, channel.Type, stored.Type)
					require.Equal(t, channel.Key, stored.Key)
					require.Equal(t, channel.BaseURL, stored.BaseURL)
					require.Equal(t, channel.Setting, stored.Setting)
					require.Equal(t, channel.OtherSettings, stored.OtherSettings)
					require.NotNil(t, stored.HeaderOverride)
					require.Equal(t, wantHeader, *stored.HeaderOverride)
				})
			}
		})
	}
}
