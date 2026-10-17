package controller

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestResponsesWebSocketPrivatePinRechecksBeforeBilling(t *testing.T) {
	for _, cache := range []bool{false, true} {
		for _, scenario := range []string{"ability revoked", "entry removed", "token model revoked", "auto group revoked", "route store unavailable", "automatic pin keeps native behavior", "unchanged private pin", "unchanged auto group"} {
			t.Run(fmt.Sprintf("cache=%t/%s", cache, scenario), func(t *testing.T) {
				var creates atomic.Int32
				fixture := newResponsesWSBillingTestWithSetup(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
					for {
						if _, _, err := ws.ReadMessage(); err != nil {
							return
						}
						n := creates.Add(1)
						terminal := fmt.Sprintf(`{"type":"response.completed","response":{"id":"route_%d","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`, n)
						if !assert.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte(terminal))) {
							return
						}
					}
				}, func(f *responsesWSBillingTest) string {
					require.NoError(t, model.DB.Model(f.user).Update("role", common.RoleAdminUser).Error)
					if scenario != "automatic pin keeps native behavior" {
						require.NoError(t, model.DB.Create(&model.TokenRouteProfile{TokenID: f.token.Id, Mode: model.TokenRouteManual, Version: 1}).Error)
						require.NoError(t, model.DB.Create(&model.TokenRouteEntry{TokenID: f.token.Id, ChannelID: f.channel.Id, Enabled: true}).Error)
						t.Cleanup(func() {
							require.NoError(t, model.DB.Where("token_id = ?", f.token.Id).Delete(&model.TokenRouteEntry{}).Error)
							require.NoError(t, model.DB.Where("token_id = ?", f.token.Id).Delete(&model.TokenRouteProfile{}).Error)
						})
					}
					if scenario == "auto group revoked" || scenario == "unchanged auto group" {
						previousGroups := setting.UserUsableGroups2JSONString()
						require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default","auto":"Auto"}`))
						t.Cleanup(func() { require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(previousGroups)) })
						f.token.Group, f.token.AutoGroups = "auto", `["default"]`
						require.NoError(t, f.token.Update())
					}
					common.MemoryCacheEnabled = cache
					if cache {
						model.InitChannelCache()
					}
					return fmt.Sprintf("Bearer sk-%s-%d", f.token.Key, f.channel.Id)
				})
				create := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, create))
				require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
				// Reload before a token edit, so the real update path preserves the
				// first response's settled quota and publishes cache invalidation.
				require.Eventually(t, func() bool {
					var token model.Token
					return model.DB.First(&token, fixture.token.Id).Error == nil && token.UsedQuota == 1000
				}, 3*time.Second, 10*time.Millisecond)
				switch scenario {
				case "ability revoked", "automatic pin keeps native behavior":
					fixture.channel.Models = "another-model"
					require.NoError(t, fixture.channel.Update())
					if cache {
						model.InitChannelCache()
					}
				case "entry removed":
					require.NoError(t, model.DB.Where("token_id = ?", fixture.token.Id).Delete(&model.TokenRouteEntry{}).Error)
				case "route store unavailable":
					require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("routing:unavailable", func(tx *gorm.DB) {
						if tx.Statement.Table == "token_route_profiles" {
							tx.AddError(fmt.Errorf("routing store unavailable"))
						}
					}))
					t.Cleanup(func() { model.DB.Callback().Query().Remove("routing:unavailable") })
				case "token model revoked", "auto group revoked":
					require.NoError(t, model.DB.First(fixture.token, fixture.token.Id).Error)
					if scenario == "token model revoked" {
						fixture.token.ModelLimitsEnabled, fixture.token.ModelLimits = true, "another-model"
					} else {
						fixture.token.AutoGroups = `["not-authorized"]`
					}
					require.NoError(t, fixture.token.Update())
				}
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, create))
				event := readResponsesWSTestEvent(t, fixture.client)
				if scenario == "automatic pin keeps native behavior" || scenario == "unchanged private pin" || scenario == "unchanged auto group" {
					require.Equal(t, "response.completed", event["type"])
					fixture.closeAndWait(t)
					require.EqualValues(t, 2, creates.Load())
					assertResponsesWSAccounting(t, fixture, []int{1000, 1000})
				} else {
					require.Equal(t, "error", event["type"])
					status, closeCode := http.StatusForbidden, websocket.ClosePolicyViolation
					if scenario == "route store unavailable" {
						status, closeCode = http.StatusInternalServerError, websocket.CloseInternalServerErr
					}
					require.EqualValues(t, status, event["status"])
					_, _, err := fixture.client.ReadMessage()
					require.True(t, websocket.IsCloseError(err, closeCode), "expected close %d, got %v", closeCode, err)
					fixture.closeAndWait(t)
					require.EqualValues(t, 1, creates.Load(), "revoked create must not reach upstream")
					assertResponsesWSAccounting(t, fixture, []int{1000})
				}
				require.EqualValues(t, 1, fixture.connections.Load())
			})
		}
	}
}

func TestResponsesWebSocketPrivatePinAbilityQueryFailure(t *testing.T) {
	for _, accepted := range []int{0, 1} {
		t.Run(fmt.Sprintf("accepted=%d", accepted), func(t *testing.T) {
			var creates atomic.Int32
			fixture := newResponsesWSBillingTestWithSetup(t, `tier("request", fixed(0.002))`, func(ws *websocket.Conn, _ *http.Request) {
				for {
					if _, _, err := ws.ReadMessage(); err != nil {
						return
					}
					creates.Add(1)
					if err := ws.WriteMessage(websocket.TextMessage, []byte(`{"type":"response.completed","response":{"id":"route_ok","status":"completed","usage":{"input_tokens":1000,"output_tokens":10,"total_tokens":1010}}}`)); err != nil {
						return
					}
				}
			}, func(f *responsesWSBillingTest) string {
				require.NoError(t, model.DB.Model(f.user).Update("role", common.RoleAdminUser).Error)
				require.NoError(t, model.DB.Create(&model.TokenRouteProfile{TokenID: f.token.Id, Mode: model.TokenRouteManual, Version: 1}).Error)
				require.NoError(t, model.DB.Create(&model.TokenRouteEntry{TokenID: f.token.Id, ChannelID: f.channel.Id, Enabled: true}).Error)
				t.Cleanup(func() {
					require.NoError(t, model.DB.Where("token_id = ?", f.token.Id).Delete(&model.TokenRouteEntry{}).Error)
					require.NoError(t, model.DB.Where("token_id = ?", f.token.Id).Delete(&model.TokenRouteProfile{}).Error)
				})
				return fmt.Sprintf("Bearer sk-%s-%d", f.token.Key, f.channel.Id)
			})
			create := []byte(`{"type":"response.create","model":"ws-billing","input":"hello"}`)
			if accepted == 1 {
				require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, create))
				require.Equal(t, "response.completed", readResponsesWSTestEvent(t, fixture.client)["type"])
				require.Eventually(t, func() bool {
					var token model.Token
					return model.DB.First(&token, fixture.token.Id).Error == nil && token.UsedQuota == 1000
				}, 3*time.Second, 10*time.Millisecond)
			}
			require.NoError(t, model.DB.Callback().Query().Before("gorm:query").Register("routing:ability-unavailable", func(tx *gorm.DB) {
				if tx.Statement.Table == "abilities" {
					tx.AddError(fmt.Errorf("private ability query diagnostic"))
				}
			}))
			t.Cleanup(func() { model.DB.Callback().Query().Remove("routing:ability-unavailable") })
			require.NoError(t, fixture.client.WriteMessage(websocket.TextMessage, create))
			event := readResponsesWSTestEvent(t, fixture.client)
			require.Equal(t, "error", event["type"])
			require.EqualValues(t, http.StatusInternalServerError, event["status"])
			require.NotContains(t, fmt.Sprint(event), "private ability query diagnostic")
			_, _, err := fixture.client.ReadMessage()
			require.True(t, websocket.IsCloseError(err, websocket.CloseInternalServerErr), "expected close 1011, got %v", err)
			fixture.closeAndWait(t)
			require.EqualValues(t, accepted, creates.Load())
			require.EqualValues(t, accepted, fixture.connections.Load())
			var quotas []int
			if accepted == 1 {
				quotas = []int{1000}
			}
			assertResponsesWSAccounting(t, fixture, quotas)
		})
	}
}
