package controller_test

import (
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/router"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/request_plugin_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func TestSwapFaceRequestPluginsRejectBeforeSubmission(t *testing.T) {
	for _, global := range []bool{true, false} {
		name := "channel_ingress"
		if global {
			name = "global_ingress"
		}
		t.Run(name, func(t *testing.T) { exerciseSwapFaceRequestPluginRejection(t, global, nil) })
	}
}

// This uses the actual public route, authentication, distribution, controller and
// JS request plugin. No test handler replaces the production submission chain.
func exerciseSwapFaceRequestPluginRejection(t *testing.T, global bool, afterRequest func(userID, channelID int)) {
	t.Helper()
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousSQLite, previousMaster := common.SQLitePath, common.IsMasterNode
	previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousMemory, previousBatch, previousRedis := common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.RedisEnabled
	previousLogConsume, previousErrorLog := common.LogConsumeEnabled, constant.ErrorLogEnabled
	previousMode := gin.Mode()
	previousPrices := ratio_setting.ModelPrice2JSONString()
	previousInstances, err := common.Marshal(request_plugin_setting.GetInstances())
	require.NoError(t, err)
	previousRegistry := jsplugin.DefaultRegistry
	t.Setenv("SQL_DSN", "")
	t.Setenv("LOG_SQL_DSN", "")
	common.SQLitePath = filepath.Join(t.TempDir(), "swapface.db")
	common.IsMasterNode, common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.RedisEnabled = false, false, false, false
	common.LogConsumeEnabled, constant.ErrorLogEnabled = true, true
	gin.SetMode(gin.TestMode)
	require.NoError(t, model.InitDB())
	database := model.DB
	model.LOG_DB = database
	sqlDB, err := database.DB()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 3*time.Second, time.Millisecond, "submission accounting must finish before closing the fixture database")
		require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: string(previousInstances)}))
		jsplugin.DefaultRegistry = previousRegistry
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(previousPrices))
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SQLitePath, common.IsMasterNode = previousSQLite, previousMaster
		common.SetMainDatabaseType(previousMainType)
		common.SetLogDatabaseType(previousLogType)
		common.MemoryCacheEnabled, common.BatchUpdateEnabled, common.RedisEnabled = previousMemory, previousBatch, previousRedis
		common.LogConsumeEnabled, constant.ErrorLogEnabled = previousLogConsume, previousErrorLog
		gin.SetMode(previousMode)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, database.AutoMigrate(&model.User{}, &model.Token{}, &model.Channel{}, &model.Ability{}, &model.Midjourney{}, &model.Task{}, &model.Log{}, &model.Model{}, &model.Vendor{}, &model.Option{}))
	// These empty tables serve the local automatic Token-route lookup; native
	// NewAPI never consults them. The production route and request are identical.
	require.NoError(t, database.Exec("CREATE TABLE token_route_profiles (token_id integer PRIMARY KEY, mode varchar(16), max_channel_ratio varchar(64), version bigint)").Error)
	require.NoError(t, database.Exec("CREATE TABLE token_route_entries (token_id integer, channel_id integer, position integer, enabled boolean, PRIMARY KEY (token_id, channel_id))").Error)
	require.NoError(t, i18n.Init())
	service.InitHttpClient()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"swap_face":0.01}`))
	registry := jsplugin.NewRegistry()
	_, err = registry.Register(`export const meta = {apiVersion: 1, key: "swapface-deny", name: "SwapFace deny", version: "1.0.0", author: {name: "Test"}};
export function ingress(ctx, req) {
 if (ctx.format !== "mj_proxy") throw new Error("unexpected protocol " + ctx.format);
 return {action: "reject", code: "blocked.swapface", message: "SwapFace blocked " + ctx.format};
}`, jsplugin.Options{})
	require.NoError(t, err)
	jsplugin.DefaultRegistry = registry
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: fmt.Sprintf(`[{"id":"swapface-deny-instance","name":"SwapFace deny","plugin":"swapface-deny","enabled":true,"global":%t}]`, global)}))

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"code":4,"description":"upstream should not receive denied requests","result":""}`)
	}))
	t.Cleanup(upstream.Close)
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	id := 72000000 + int(h.Sum32()%10000000)
	user := model.User{Id: id, Username: fmt.Sprintf("swapface%d", id), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 1000000}
	// Unknown JSON fields are ignored in native NewAPI. The local release test
	// separately observes the real max_concurrency=1 admission and release.
	require.NoError(t, common.Unmarshal([]byte(`{"max_concurrency":1}`), &user))
	require.NoError(t, database.Create(&user).Error)
	token := model.Token{Id: id, UserId: id, Key: fmt.Sprintf("swapfacetoken%d", id), Name: "swapface-token", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000}
	require.NoError(t, database.Create(&token).Error)
	channel := model.Channel{Id: id, Name: "swapface-channel", Type: constant.ChannelTypeMidjourney, Status: common.ChannelStatusEnabled, Models: "swap_face", BaseURL: &upstream.URL, Key: "fixtureupstreamkey"}
	require.NoError(t, common.Unmarshal([]byte(`{"max_concurrency":1}`), &channel))
	if !global {
		channel.SetSetting(dto.ChannelSettings{RequestPlugins: []string{"swapface-deny-instance"}})
	}
	require.NoError(t, database.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(nil))

	var walletWrites atomic.Int32
	require.NoError(t, database.Callback().Update().Before("gorm:update").Register("swapface:observe_wallet_updates", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" || tx.Statement.Table == "tokens" {
			walletWrites.Add(1)
		}
	}))
	engine := gin.New()
	router.SetRelayRouter(engine)
	request := httptest.NewRequest(http.MethodPost, "/mj/insight-face/swap", strings.NewReader(`{"sourceBase64":"c291cmNl","targetBase64":"dGFyZ2V0"}`))
	request.Header.Set("Authorization", "Bearer sk-"+token.Key)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Eventually(t, func() bool { return gopool.WorkerCount() == 0 }, 3*time.Second, time.Millisecond)
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Equal(t, "request_rejected", gjson.Get(response.Body.String(), "type").String())
	assert.Contains(t, gjson.Get(response.Body.String(), "description").String(), "SwapFace blocked mj_proxy (code: blocked.swapface)")
	assert.EqualValues(t, constant.MjRequestError, gjson.Get(response.Body.String(), "code").Int())
	assert.Zero(t, upstreamCalls.Load(), "ingress rejection must not call the upstream")
	assert.Zero(t, walletWrites.Load(), "ingress rejection must not reserve, settle, or refund wallet or Token quota")
	require.NoError(t, database.First(&user, id).Error)
	require.NoError(t, database.First(&token, id).Error)
	require.NoError(t, database.First(&channel, id).Error)
	assert.Equal(t, 1000000, user.Quota)
	assert.Zero(t, user.UsedQuota)
	assert.Zero(t, user.RequestCount)
	assert.Equal(t, 1000000, token.RemainQuota)
	assert.Zero(t, token.UsedQuota)
	assert.Zero(t, channel.UsedQuota)
	for _, kind := range []any{&model.Midjourney{}, &model.Task{}} {
		var count int64
		require.NoError(t, database.Model(kind).Count(&count).Error)
		assert.Zero(t, count, "rejected requests must not create a task")
	}
	var monetaryLogs int64
	require.NoError(t, database.Model(&model.Log{}).Where("type IN ?", []int{model.LogTypeConsume, model.LogTypeRefund}).Count(&monetaryLogs).Error)
	assert.Zero(t, monetaryLogs)
	var errors []model.Log
	require.NoError(t, database.Where("type = ?", model.LogTypeError).Find(&errors).Error)
	if assert.Len(t, errors, 1, "the request-plugin rejection must be audited") {
		assert.Equal(t, "swapface-deny", gjson.Get(errors[0].Other, "request_plugins.0.plugin").String())
		assert.Equal(t, "ingress", gjson.Get(errors[0].Other, "request_plugins.0.stage").String())
		assert.Equal(t, "blocked.swapface", gjson.Get(errors[0].Other, "request_plugins.0.rejected.code").String())
		expectedChannel := 0
		if !global {
			expectedChannel = id
		}
		assert.Equal(t, expectedChannel, errors[0].ChannelId)
	}
	if afterRequest != nil {
		afterRequest(id, id)
	}
}
