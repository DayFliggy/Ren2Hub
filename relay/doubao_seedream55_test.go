package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const seedream55BillingModel = "doubao-seedream-5-5-261028"

func TestSeedream55RealWalletReservationAndSettlement(t *testing.T) {
	service.InitHttpClient()
	const wallet = 20_000_000
	const expression = `tier("images", u("images_up_to_1_5k") * 0.02 + u("images_above_1_5k") * 0.05 + u("input_images") * 0.01)`
	const mixedImages = `{"data":[{"url":"https://images.example/base.png","size":"2048x2048"},{"url":"https://images.example/one.png","size":"1500x1740"},{"b64_json":"aW1hZ2U=","size":"1024x1024"},{"error":{"code":"blocked"}}],"usage":{"generated_images":3,"input_images":1}}`
	hundredImages := make([]map[string]any, 100)
	for i := range hundredImages {
		hundredImages[i] = map[string]any{"url": "https://images.example/layer.png", "size": "1024x1024"}
	}
	hundredPayload, err := common.Marshal(map[string]any{"data": hundredImages, "usage": map[string]any{"generated_images": 100, "input_images": 1}})
	require.NoError(t, err)
	for _, tc := range []struct {
		name                  string
		expression            bool
		maxImages             int
		wallet                int
		body                  string
		status                int
		wantHeld, wantCharged int
		wantError             string
	}{
		{"per image fewer results", false, 0, wallet, mixedImages, 200, 2_250_000, 225_000, ""},
		{"per image minimum", false, 2, wallet, `{"data":[{"url":"https://images.example/1.png","size":"1024x1024"}],"usage":{"generated_images":1}}`, 200, 150_000, 75_000, ""},
		{"per image maximum", false, 100, wallet, mixedImages, 200, 7_500_000, 225_000, ""},
		{"per image delivers all hundred", false, 100, wallet, string(hundredPayload), 200, 7_500_000, 7_500_000, ""},
		{"expression fewer mixed tiers", true, 0, wallet, mixedImages, 200, 1_132_500, 75_000, ""},
		{"expression maximum", true, 100, wallet, mixedImages, 200, 3_757_500, 75_000, ""},
		{"expression delivers all hundred", true, 100, wallet, string(hundredPayload), 200, 3_757_500, 1_507_500, ""},
		{"per image mismatched count keeps reservation", false, 0, wallet, `{"data":[{"url":"https://images.example/1.png","size":"1024x1024"}],"usage":{"generated_images":2}}`, 200, 2_250_000, 2_250_000, ""},
		{"expression mismatched count keeps reservation", true, 0, wallet, `{"data":[{"url":"https://images.example/1.png","size":"1024x1024"}],"usage":{"generated_images":2}}`, 200, 1_132_500, 1_132_500, ""},
		{"unaffordable maximum never sent", false, 100, 7_499_999, mixedImages, 200, 0, 0, "insufficient_user_quota"},
		{"unaffordable expression never sent", true, 0, 1_132_499, mixedImages, 200, 0, 0, "insufficient_user_quota"},
		{"upstream error refunds", false, 0, wallet, `{"error":{"message":"upstream failed"}}`, 502, 2_250_000, 0, "fail_to_fetch_task"},
		{"no delivered image refunds", true, 0, wallet, `{"data":[{"error":{"code":"blocked"}}],"usage":{"generated_images":0}}`, 200, 1_132_500, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saveBillingConfig(t)
			savedPrices := ratio_setting.ModelPrice2JSONString()
			t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(savedPrices)) })
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"`+seedream55BillingModel+`":0.1}`))
			expressions := map[string]string{}
			if tc.expression {
				expressions["doubao::"+seedream55BillingModel] = expression
			}
			encodedExpressions, err := common.Marshal(expressions)
			require.NoError(t, err)
			require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
				billing_setting.PluginBillingExprOption: string(encodedExpressions),
				"billing_setting.billing_mode":          `{}`, "billing_setting.billing_expr": `{}`,
			}))
			c, info := newTaskSubmitContext(t, seedream55BillingModel, `{"`+seedream55BillingModel+`":"ep-seedream55-test"}`)
			previousBatch := common.BatchUpdateEnabled
			common.BatchUpdateEnabled = false
			t.Cleanup(func() { common.BatchUpdateEnabled = previousBatch })
			// Refunds enqueue cache updates after the wallet write. Keep the
			// database and Redis fixture alive until those nested tasks finish.
			// Register after the fixture so this runs before its LIFO cleanup.
			t.Cleanup(func() {
				require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 2*time.Second, time.Millisecond,
					"billing background work must finish before restoring the database and Redis settings")
			})
			require.NoError(t, model.DB.Model(&model.User{}).Where("id = ?", info.UserId).Update("quota", tc.wallet).Error)
			token := model.Token{UserId: info.UserId, Key: "seedream55-test-token", Status: common.TokenStatusEnabled, RemainQuota: wallet}
			require.NoError(t, model.DB.Create(&token).Error)
			info.TokenId, info.TokenKey = token.Id, token.Key
			info.OriginModelName = seedream55BillingModel
			info.Action = "image_to_image"
			c.Set("channel_ratio", "1.5")
			c.Request = httptest.NewRequest(http.MethodPost, "/doubao/api/v3/images/generations", nil)
			request := map[string]any{"model": seedream55BillingModel, "image": "https://reference.example/input.png", "layer_decomposition": true, "size": "auto"}
			if tc.maxImages != 0 {
				request["layer_decomposition_options"] = map[string]any{"max_images": tc.maxImages}
			}
			c.Set("task_request", request)
			source, err := builtinplugins.Source("doubao")
			require.NoError(t, err)
			registry := jsplugin.NewRegistry()
			plugin, err := registry.RegisterFactory(source, jsplugin.Options{Key: "doubao"})
			require.NoError(t, err)
			c.Set(jsplugin.ContextKeyPinnedPlugin, jsplugin.PinnedPlugin{Generation: registry.Generation(), Plugin: plugin})
			c.Set("task_plugin_key", "doubao")
			var sent atomic.Int32
			type observedRequest struct {
				path                  string
				body                  []byte
				userQuota, tokenQuota int
				err                   error
			}
			observed := make(chan observedRequest, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sent.Add(1)
				body, readErr := io.ReadAll(r.Body)
				var user model.User
				var heldToken model.Token
				if readErr == nil {
					readErr = model.DB.First(&user, info.UserId).Error
				}
				if readErr == nil {
					readErr = model.DB.First(&heldToken, token.Id).Error
				}
				observed <- observedRequest{r.URL.Path, body, user.Quota, heldToken.RemainQuota, readErr}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()
			common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, server.URL)
			common.SetContextKey(c, constant.ContextKeyChannelKey, "seedream55-local-key")
			result, taskErr := RelayTaskSubmit(c, info)
			if tc.wantHeld == 0 {
				require.NotNil(t, taskErr)
				assert.Equal(t, tc.wantError, taskErr.Code)
				assert.Zero(t, sent.Load(), "insufficient funds must stop before an upstream request")
				assert.Nil(t, info.Billing)
			} else {
				require.EqualValues(t, 1, sent.Load())
				outbound := <-observed
				require.NoError(t, outbound.err)
				assert.Equal(t, "/api/v3/images/generations", outbound.path)
				var body map[string]any
				require.NoError(t, common.Unmarshal(outbound.body, &body))
				assert.Equal(t, "ep-seedream55-test", body["model"], "the channel alias must retain the declared model's pricing profile")
				assert.Equal(t, tc.wallet-tc.wantHeld, outbound.userQuota)
				assert.Equal(t, wallet-tc.wantHeld, outbound.tokenQuota)
				assert.Equal(t, tc.wantHeld, info.Billing.GetPreConsumedQuota())
				if tc.wantCharged == 0 {
					require.NotNil(t, taskErr)
					if tc.wantError != "" {
						assert.Equal(t, tc.wantError, taskErr.Code)
					}
					info.Billing.Refund(c)
					info.Billing.Refund(c)
				} else {
					require.Nil(t, taskErr, "%+v", taskErr)
					require.NotNil(t, result.Immediate)
					assert.Equal(t, model.TaskStatusSuccess, result.Immediate.Status)
					assert.Equal(t, tc.wantCharged, result.Quota)
					require.NoError(t, service.SettleBilling(c, info, result.Quota))
					require.NoError(t, service.SettleBilling(c, info, result.Quota))
					info.Billing.Refund(c)
					assert.False(t, info.Billing.NeedsRefund())
				}
			}
			require.Eventually(t, func() bool {
				var user model.User
				var finalToken model.Token
				if model.DB.First(&user, info.UserId).Error != nil || model.DB.First(&finalToken, token.Id).Error != nil {
					return false
				}
				return user.Quota == tc.wallet-tc.wantCharged && finalToken.RemainQuota == wallet-tc.wantCharged && finalToken.UsedQuota == tc.wantCharged
			}, 2*time.Second, time.Millisecond, "wallet and Token must retain exactly one final charge (or one refund)")
		})
	}
}

func TestSeedream55FactoryUpgradePreservesOverrideAndTaskProvenance(t *testing.T) {
	db := setupRelayChannelDB(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.TaskPlugin{}, &model.SystemTask{}))
	source, err := builtinplugins.Source("doubao")
	require.NoError(t, err)
	registry := jsplugin.NewRegistry()
	// A saved uploaded version is immutable and remains active across the
	// factory upgrade; a newly uploaded version starts inactive.
	legacySource := strings.Replace(source, `version: "1.5.0"`, `version: "1.4.0"`, 1)
	require.NotEqual(t, source, legacySource)
	oldPlugin, err := registry.Register(legacySource, jsplugin.Options{Key: "doubao"})
	require.NoError(t, err)
	oldRow := model.TaskPlugin{Key: "doubao", APIVersion: 1, Version: "1.4.0", Source: model.LongText(legacySource), SourceHash: "legacy-source", Enabled: true}
	require.NoError(t, model.SaveTaskPlugin(&oldRow))
	newRow := model.TaskPlugin{Key: "doubao", APIVersion: 1, Version: "1.5.0", Source: model.LongText(source), SourceHash: "new-source", Enabled: true}
	require.NoError(t, model.SaveTaskPlugin(&newRow))
	assert.False(t, newRow.Active)
	oldGeneration := registry.Generation()
	oldPinned := jsplugin.PinnedPlugin{Generation: oldGeneration, Plugin: oldPlugin}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(jsplugin.ContextKeyPinnedPlugin, oldPinned)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
	oldTask := &model.Task{TaskID: "task_seedream55_previous", UserId: 42, ChannelId: 7, Platform: "doubao", Action: "image_to_image", Status: model.TaskStatusSuccess,
		Properties:  model.Properties{OriginModelName: "doubao-seedream-5-0-pro-260628", UpstreamModelName: "ep-old-image"},
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "upstream-old-image", Key: "local-original-account", CredentialAccount: "account-original", Execution: service.TaskExecutionSnapshotFromContext(c)},
		Data:        []byte(`{"data":[{"url":"https://images.example/old.png","size":"1024x1024"}]}`),
	}
	require.NoError(t, db.Create(oldTask).Error)
	factory, err := registry.RegisterFactory(source, jsplugin.Options{Key: "doubao"})
	require.NoError(t, err)
	assert.Equal(t, "1.5.0", factory.Meta.Version)
	active, ok := registry.Generation().Get("doubao")
	require.True(t, ok)
	assert.Same(t, oldPlugin, active, "factory updates must not replace an activated upload")
	var activeRows []model.TaskPlugin
	require.NoError(t, db.Where("active = ?", true).Find(&activeRows).Error)
	require.Len(t, activeRows, 1)
	assert.Equal(t, "1.4.0", activeRows[0].Version)
	assert.Equal(t, model.LongText(legacySource), activeRows[0].Source)
	assert.Equal(t, "1.4.0", service.TaskExecutionSnapshotFromContext(c).TaskPlugin.Version)
	require.NoError(t, registry.Unregister("doubao"))
	active, ok = registry.Generation().Get("doubao")
	require.True(t, ok)
	assert.Same(t, factory, active)
	var restored model.Task
	require.NoError(t, db.First(&restored, oldTask.ID).Error)
	assert.Equal(t, oldTask.PrivateData, restored.PrivateData)
	assert.Equal(t, "1.4.0", restored.PrivateData.Execution.TaskPlugin.Version)
	assert.Equal(t, "account-original", restored.PrivateData.CredentialAccount)
	artifacts, err := taskplugin.New(factory).ListArtifacts(&restored)
	require.NoError(t, err)
	require.Len(t, artifacts, 1, "the current factory must still restore a previous model's stored output")
	assert.Equal(t, "image-1", artifacts[0].Key)
	assert.Equal(t, oldGeneration.Number, restored.PrivateData.Execution.TaskPlugin.Generation)
	// The same built-in artifact path receives the immutable producing version,
	// even though execution uses the currently selected factory.
	observedSource := strings.Replace(source, "export function listArtifacts(task) {", "export function listArtifacts(task) { if (task.producerVersion !== '1.4.0') throw new Error('producer version was replaced');", 1)
	require.NotEqual(t, source, observedSource)
	observedPlugin, err := jsplugin.CompilePlugin(observedSource, jsplugin.Options{Key: "doubao"})
	require.NoError(t, err)
	_, err = taskplugin.New(observedPlugin).ListArtifacts(&restored)
	require.NoError(t, err)
}
