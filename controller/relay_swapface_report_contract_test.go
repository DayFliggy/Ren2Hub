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

func TestSwapFaceRequestPluginBodyRewriteAndReport(t *testing.T) {
	exerciseSwapFaceBodyAndReport(t, "rewrite")
}

func TestSwapFaceRequestPluginRejectionReports(t *testing.T) {
	for _, mode := range []string{"global_rejection", "channel_rejection"} {
		t.Run(mode, func(t *testing.T) { exerciseSwapFaceBodyAndReport(t, mode) })
	}
}

// Exercise the actual registered route and controller defer. Reports are
// observed at a real local HTTP sink, not inferred from database audit logs.
func exerciseSwapFaceBodyAndReport(t *testing.T, mode string) {
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
	reports := make(chan []byte, 8)
	reportSink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/report", r.URL.Path)
		body, readErr := io.ReadAll(r.Body)
		assert.NoError(t, readErr)
		select {
		case reports <- body:
		default:
			t.Error("unexpected extra report hook calls")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(reportSink.Close)
	registry := jsplugin.NewRegistry()
	_, err = registry.Register(`export const meta = {apiVersion: 1, key: "swapface-observe", name: "SwapFace observe", version: "1.0.0", author: {name: "Test"},
 configSchema: {type:"object", properties: {
  url:{type:"string",title:"Report URL",format:"url"},
  reject:{type:"boolean",title:"Reject"},
  field:{type:"string",title:"Field"}, value:{type:"string",title:"Value"}
 }}};
export function ingress(ctx, req) {
 if (ctx.format !== "mj_proxy") throw new Error("unexpected protocol " + ctx.format);
 if (ctx.config.reject) return {action:"reject",code:"blocked.swapface",message:"SwapFace report fixture rejection"};
 return {action:"continue",effects:[{type:"json.patch",ops:[{op:"replace",path:"/"+ctx.config.field,value:ctx.config.value}]}]};
}
export function report(ctx, record) {
 utils.fetch({url:ctx.config.url,method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({instance:ctx.instance.id,ctx,record})});
}`, jsplugin.Options{})
	require.NoError(t, err)
	jsplugin.DefaultRegistry = registry
	instances := []map[string]any{
		{"id": "swapface-global", "name": "Global", "plugin": "swapface-observe", "enabled": true, "global": true,
			"config": map[string]any{"url": reportSink.URL + "/report", "reject": mode == "global_rejection", "field": "sourceBase64", "value": "bW9kaWZpZWQtc291cmNl"}},
		{"id": "swapface-channel", "name": "Channel", "plugin": "swapface-observe", "enabled": true, "global": false,
			"config": map[string]any{"url": reportSink.URL + "/report", "reject": mode == "channel_rejection", "field": "targetBase64", "value": "bW9kaWZpZWQtdGFyZ2V0"}},
	}
	encoded, err := common.Marshal(instances)
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{request_plugin_setting.InstancesOption: string(encoded)}))

	var upstreamCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls.Add(1)
		assert.Equal(t, "rewrite", mode, "rejections must never reach a supplier")
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/mj/insight-face/swap", r.URL.Path)
		body, readErr := io.ReadAll(r.Body)
		assert.NoError(t, readErr)
		assert.Equal(t, "bW9kaWZpZWQtc291cmNl", gjson.GetBytes(body, "sourceBase64").String(), "global ingress rewrite reaches real provider dispatch")
		assert.Equal(t, "bW9kaWZpZWQtdGFyZ2V0", gjson.GetBytes(body, "targetBase64").String(), "channel TaskAttempt rewrite reaches real provider dispatch")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":1,"description":"accepted","result":"swapface-report-task"}`)
	}))
	t.Cleanup(upstream.Close)
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.Name()))
	id := 72000000 + int(h.Sum32()%10000000)
	user := model.User{Id: id, Username: fmt.Sprintf("swapface%d", id), Role: common.RoleCommonUser, Status: common.UserStatusEnabled, Quota: 1000000, Setting: `{"billing_preference":"wallet_only"}`}
	// Unknown JSON fields are ignored in native NewAPI. The local release test
	// separately observes the real max_concurrency=1 admission and release.
	require.NoError(t, common.Unmarshal([]byte(`{"max_concurrency":1}`), &user))
	require.NoError(t, database.Create(&user).Error)
	token := model.Token{Id: id, UserId: id, Key: fmt.Sprintf("swapfacetoken%d", id), Name: "swapface-token", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000}
	require.NoError(t, database.Create(&token).Error)
	channel := model.Channel{Id: id, Name: "swapface-channel", Type: constant.ChannelTypeMidjourney, Status: common.ChannelStatusEnabled, Models: "swap_face", BaseURL: &upstream.URL, Key: "fixtureupstreamkey"}
	require.NoError(t, common.Unmarshal([]byte(`{"max_concurrency":1}`), &channel))
	channel.SetSetting(dto.ChannelSettings{RequestPlugins: []string{"swapface-channel"}})
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
	wantStatus, wantQuota, wantTasks, wantReports := http.StatusBadRequest, 0, int64(0), 2
	if mode == "rewrite" {
		wantStatus, wantQuota, wantTasks = http.StatusOK, 5000, 1
		assert.EqualValues(t, 1, upstreamCalls.Load())
		assert.EqualValues(t, 1, gjson.Get(response.Body.String(), "code").Int())
	} else {
		assert.Zero(t, upstreamCalls.Load())
		assert.Zero(t, walletWrites.Load(), "plugin rejection must not reserve or refund funds")
		assert.Equal(t, "request_rejected", gjson.Get(response.Body.String(), "type").String())
	}
	if mode == "global_rejection" {
		wantReports = 1
	}
	assert.Equal(t, wantStatus, response.Code)
	require.NoError(t, database.First(&user, id).Error)
	require.NoError(t, database.First(&token, id).Error)
	require.NoError(t, database.First(&channel, id).Error)
	assert.Equal(t, 1000000-wantQuota, user.Quota)
	assert.Equal(t, wantQuota, user.UsedQuota)
	assert.Equal(t, int(wantTasks), user.RequestCount)
	assert.Equal(t, 1000000-wantQuota, token.RemainQuota)
	assert.Equal(t, wantQuota, token.UsedQuota)
	assert.Equal(t, int64(wantQuota), channel.UsedQuota)
	var tasks, refunds, consumes int64
	require.NoError(t, database.Model(&model.Midjourney{}).Count(&tasks).Error)
	assert.Equal(t, wantTasks, tasks)
	require.NoError(t, database.Model(&model.Task{}).Count(&tasks).Error)
	assert.Zero(t, tasks)
	require.NoError(t, database.Model(&model.Log{}).Where("type = ?", model.LogTypeConsume).Count(&consumes).Error)
	require.NoError(t, database.Model(&model.Log{}).Where("type = ?", model.LogTypeRefund).Count(&refunds).Error)
	assert.Equal(t, wantTasks, consumes)
	assert.Zero(t, refunds)
	seen := map[string]bool{}
	for i := 0; i < wantReports; i++ {
		var raw []byte
		select {
		case raw = <-reports:
		case <-time.After(5 * time.Second):
			t.Fatal("deferred report hook did not reach the local HTTP sink")
		}
		record := gjson.ParseBytes(raw)
		instance := record.Get("instance").String()
		assert.Contains(t, []string{"swapface-global", "swapface-channel"}, instance)
		assert.False(t, seen[instance], "report hook must execute once per configured instance")
		seen[instance] = true
		assert.Equal(t, "mj_proxy", record.Get("ctx.format").String())
		assert.Equal(t, "/mj/insight-face/swap", record.Get("ctx.path").String())
		assert.Equal(t, "swap_face", record.Get("ctx.model").String())
		assert.EqualValues(t, id, record.Get("record.user.id").Int())
		assert.EqualValues(t, id, record.Get("record.token.id").Int())
		assert.EqualValues(t, wantStatus, record.Get("record.response.status").Int())
		assert.EqualValues(t, wantQuota, record.Get("record.cost.quota").Int())
		if mode == "rewrite" {
			assert.False(t, record.Get("record.error").Exists())
			paths := []string{}
			for _, entry := range record.Get("record.requestPlugins").Array() {
				assert.Equal(t, "ingress", entry.Get("stage").String())
				for _, change := range entry.Get("changes").Array() {
					paths = append(paths, change.Get("path").String())
				}
			}
			assert.ElementsMatch(t, []string{"sourceBase64", "targetBase64"}, paths)
		} else {
			assert.EqualValues(t, 400, record.Get("record.error.status").Int())
			assert.Equal(t, "request_rejected", record.Get("record.error.code").String())
		}
	}
	assert.True(t, seen["swapface-global"])
	assert.Equal(t, mode != "global_rejection", seen["swapface-channel"])
	select {
	case extra := <-reports:
		t.Errorf("unexpected extra report: %s", extra)
	case <-time.After(50 * time.Millisecond):
	}
}
