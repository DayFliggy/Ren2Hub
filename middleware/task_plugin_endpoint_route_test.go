package middleware

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	appI18n "github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

const endpointRouteUserID = 7

// endpointRouteDatabase follows the upstream shared-endpoint matrix: every
// table gets a per-run prefix so a shared test DSN stays isolated.
func endpointRouteDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	dsn := ""
	if dialect != "sqlite" {
		dsn = os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
		if dsn == "" {
			t.Skip("database test DSN not configured")
		}
	}
	previousDB, previousMain, previousLog := model.DB, common.MainDatabaseType(), common.LogDatabaseType()
	previousPath, previousMaster := common.SQLitePath, common.IsMasterNode
	previousCache, previousRedis := common.MemoryCacheEnabled, common.RedisEnabled
	common.SQLitePath, common.IsMasterNode = t.TempDir()+"/routing.db", false
	common.RedisEnabled = false
	t.Setenv("SQL_DSN", dsn)
	require.NoError(t, model.InitDB())
	database := model.DB
	database.Config.NamingStrategy = schema.NamingStrategy{TablePrefix: "endpoint_route_" + common.GetUUID()[:16] + "_"}
	tables := []any{&model.Ability{}, &model.Channel{}, &model.Task{}, &model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}}
	require.NoError(t, database.AutoMigrate(tables...))
	t.Cleanup(func() {
		common.MemoryCacheEnabled = true
		require.NoError(t, database.Migrator().DropTable(tables...))
		model.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.SQLitePath, common.IsMasterNode = previousPath, previousMaster
		common.MemoryCacheEnabled, common.RedisEnabled = previousCache, previousRedis
		if previousDB != nil {
			model.InitChannelCache()
		}
		sqlDB, err := database.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	var version string
	query := "SELECT version()"
	if dialect == "sqlite" {
		query = "SELECT sqlite_version()"
	}
	require.NoError(t, database.Raw(query).Scan(&version).Error)
	t.Logf("database version: %s", version)
	return database
}

type endpointRouteCase struct {
	name, path, model        string
	pluginType               int
	ordinaryType             int
	ordinaryPriority         int64
	pluginPriority           int64
	pluginOnly, wantPlugin   bool
	multipart, stream        bool
	pinOrdinary, pinPlugin   bool
	wrongModel               bool
	disabled, unrelated      bool
	decoderRejects, syncOnly bool
	originPin, retry, shared bool
	observationContext       string
	mapped                   bool
	// manual lists the Token's ordered manual route by fixture name;
	// disabledEntry marks one of those entries as disabled.
	manual        []string
	disabledEntry string
	// ordinaryBusy holds the only permit of the ordinary channel; userBusy
	// holds the user's only permit so admission rejects the request.
	ordinaryBusy, pluginBusy, userBusy bool
	wantStatus                         int
	wantErrorCode                      string
	// want names the expected fixture when it is not the default winner.
	want string
}

func endpointRouteCases() []endpointRouteCase {
	return []endpointRouteCase{
		{name: "ordinary images bypass plugin decoder", decoderRejects: true},
		{name: "ordinary images win by priority", ordinaryPriority: 20},
		{name: "plugin images win by priority", pluginPriority: 20, wantPlugin: true},
		{name: "type 61 plugin remains usable", pluginOnly: true, wantPlugin: true},
		{name: "bound New API uses plugin", pluginType: constant.ChannelTypeNewAPI, pluginPriority: 20, wantPlugin: true},
		{name: "unbound New API uses ordinary relay", ordinaryType: constant.ChannelTypeNewAPI, decoderRejects: true},
		{name: "unrelated type 61 cannot steal request", unrelated: true, pluginPriority: 20},
		{name: "disabled plugin channel is ignored", disabled: true, pluginPriority: 20},
		{name: "plugin without requested model is ignored", wrongModel: true, pluginPriority: 20},
		{name: "ordinary channel pin wins", pinOrdinary: true, pluginPriority: 20},
		{name: "plugin channel pin wins", pinPlugin: true, wantPlugin: true},
		{name: "ordinary model spelling is preserved", model: "CHANNEL-ROUTE-MODEL", decoderRejects: true},
		{name: "mapped plugin alias remains usable", model: "friendly-image", mapped: true, pluginOnly: true, wantPlugin: true},
		{name: "ordinary alias is not rewritten by plugin", model: "friendly-image", mapped: true, decoderRejects: true},
		{name: "ordinary multipart edits are preserved", path: "/v1/images/edits", multipart: true, decoderRejects: true},
		{name: "plugin multipart edits remain usable", path: "/v1/images/edits", multipart: true, pluginOnly: true, wantPlugin: true},
		{name: "ordinary Responses bypass unsupported plugin mode", path: "/v1/responses", stream: true, syncOnly: true},
		{name: "plugin Responses remain usable", path: "/v1/responses", pluginOnly: true, wantPlugin: true},
		{name: "plugin stream flag survives model resolution", path: "/v1/responses", stream: true, pluginPriority: 20, wantPlugin: true, want: "plugin"},
		{name: "ordinary videos bypass plugin decoder", path: "/v1/videos", multipart: true, decoderRejects: true},
		{name: "video preselection uses submit mode observations", path: "/v1/videos", multipart: true, ordinaryPriority: -1, pluginPriority: 0, observationContext: "video"},
		{name: "plugin videos remain usable", path: "/v1/videos", multipart: true, pluginOnly: true, wantPlugin: true},
		{name: "selected plugin retains validation errors", pluginOnly: true, decoderRejects: true, wantStatus: http.StatusBadRequest},
		{name: "selected plugin retains mode errors", path: "/v1/responses", pluginOnly: true, stream: true, syncOnly: true, wantStatus: http.StatusBadRequest},
		{name: "stream preselection uses stream observations", path: "/v1/responses", stream: true, ordinaryPriority: -1, pluginPriority: 0, observationContext: "stream"},
		{name: "origin task still pins its channel", pluginOnly: true, originPin: true, wantPlugin: true, want: "alternate"},
		{name: "ordinary retries stay on ordinary channels", ordinaryPriority: 20, pluginPriority: 10, retry: true},
		{name: "plugin retries stay on plugin channels", pluginPriority: 20, retry: true, wantPlugin: true},
		// Type-61 plugins already fail the identity filter on ordinary retries;
		// a bound New API channel only stays out through the relay-only filter.
		{name: "ordinary retries skip bound New API plugin", pluginType: constant.ChannelTypeNewAPI, ordinaryPriority: 20, pluginPriority: 10, retry: true},
		{name: "rejected shared candidate still allows another plugin", pluginOnly: true, shared: true, decoderRejects: true, pluginPriority: 20, wantPlugin: true, want: "alternate"},
		// Ren2Hub Token routes replace upstream groups: the ordered manual list
		// decides between ordinary and plugin channels before priority does.
		{name: "manual route order beats ordinary priority", manual: []string{"plugin", "ordinary"}, ordinaryPriority: 20, wantPlugin: true},
		{name: "manual route order beats plugin priority", manual: []string{"ordinary", "plugin"}, pluginPriority: 20, decoderRejects: true},
		{name: "manual ordinary-only route bypasses plugin", manual: []string{"ordinary"}, pluginPriority: 20, decoderRejects: true},
		{name: "manual plugin-only route uses plugin", manual: []string{"plugin"}, ordinaryPriority: 20, wantPlugin: true},
		{name: "disabled manual entry falls to next channel", manual: []string{"ordinary", "plugin"}, disabledEntry: "ordinary", wantPlugin: true},
		{name: "manual ordinary retries skip listed plugin", manual: []string{"ordinary", "plugin", "alternate"}, retry: true},
		{name: "manual ordinary retries skip listed New API plugin", pluginType: constant.ChannelTypeNewAPI, manual: []string{"ordinary", "plugin", "alternate"}, retry: true},
		{name: "manual plugin retries skip listed ordinary", manual: []string{"plugin", "ordinary", "alternate"}, retry: true, wantPlugin: true},
		{name: "busy ordinary channel lets plugin compete", ordinaryPriority: 20, ordinaryBusy: true, wantPlugin: true},
		{name: "user admission still precedes channel permit", userBusy: true, wantStatus: http.StatusTooManyRequests, wantErrorCode: "user_concurrency_exceeded"},
		{name: "user admission wins when every channel is busy", ordinaryBusy: true, pluginBusy: true, userBusy: true, decoderRejects: true, wantStatus: http.StatusTooManyRequests, wantErrorCode: "user_concurrency_exceeded"},
		// Pre-selection failures that are not "no channel" decide the request
		// before the plugin decoder can replace them with its own error.
		{name: "all busy channels report capacity before plugin decoding", ordinaryBusy: true, pluginBusy: true, decoderRejects: true, wantStatus: http.StatusTooManyRequests},
	}
}

func TestRouteTaskPluginEndpointUsesSelectedChannel(t *testing.T) {
	require.NoError(t, appI18n.Init())
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			database := endpointRouteDatabase(t, dialect)
			for _, memoryCache := range []bool{false, true} {
				t.Run(fmt.Sprintf("memory_cache=%t", memoryCache), func(t *testing.T) {
					common.MemoryCacheEnabled = memoryCache
					for _, tc := range endpointRouteCases() {
						t.Run(tc.name, func(t *testing.T) { runEndpointRouteCase(t, database, tc) })
					}
				})
			}
		})
	}
}

func runEndpointRouteCase(t *testing.T, database *gorm.DB, tc endpointRouteCase) {
	for _, table := range []any{&model.Ability{}, &model.Channel{}, &model.Task{}, &model.Token{}, &model.TokenRouteProfile{}, &model.TokenRouteEntry{}} {
		require.NoError(t, database.Unscoped().Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error)
	}
	require.NoError(t, database.Create(&model.Token{Id: endpointRouteUserID, UserId: endpointRouteUserID, Key: "endpoint-route-fixture", Status: common.TokenStatusEnabled}).Error)
	key := "channel-route-test"
	path := common.GetStringIfEmpty(tc.path, "/v1/images/generations")
	modelName := common.GetStringIfEmpty(tc.model, "channel-route-model")
	decode := `return {model: ctx.model, action: "generate", requestBody: {normalized: true}};`
	if tc.decoderRejects {
		decode = `throw new Error("plugin rejected this request");`
	} else if tc.originPin {
		decode = `return {model: ctx.model, originTaskIds: ["task-route-origin"], requestBody: {normalized: true}};`
	}
	source := taskProtocolPluginSource(key, "1.0.0", `["channel-route-model"]`, path, decode)
	if tc.syncOnly {
		source = taskResponsesPluginSource(key, 0, `["channel-route-model"]`, `["sync"]`, `renderFinal: function() { return {}; },`, decode)
	}
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })

	ordinary := &model.Channel{Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Name: "ordinary", Key: "ordinary-key", Models: modelName, Priority: common.GetPointer(tc.ordinaryPriority + 1)}
	if tc.ordinaryType != 0 {
		ordinary.Type = tc.ordinaryType
	}
	if tc.ordinaryBusy {
		ordinary.MaxConcurrency = 1
	}
	plugin := &model.Channel{Type: constant.ChannelTypeTaskPlugin, Status: common.ChannelStatusEnabled, Name: "plugin", Key: "plugin-key", Models: modelName, Priority: &tc.pluginPriority}
	if tc.pluginType != 0 {
		plugin.Type = tc.pluginType
	}
	plugin.SetSetting(kitdto.ChannelSettings{TaskPluginKey: key})
	if tc.pluginBusy {
		plugin.MaxConcurrency = 1
	}
	if tc.mapped {
		plugin.ModelMapping = common.GetPointer(`{"friendly-image":"channel-route-model"}`)
	}
	if tc.unrelated {
		plugin.SetSetting(kitdto.ChannelSettings{TaskPluginKey: "unrelated"})
	}
	if tc.wrongModel {
		plugin.Models = "another-model"
	}
	if tc.disabled {
		plugin.Status = common.ChannelStatusManuallyDisabled
	}
	named := map[string]*model.Channel{"plugin": plugin}
	channels := []*model.Channel{plugin}
	if !tc.pluginOnly {
		named["ordinary"] = ordinary
		channels = append(channels, ordinary)
	}
	if tc.originPin || tc.retry || tc.shared {
		alternate := &model.Channel{Type: ordinary.Type, Status: common.ChannelStatusEnabled, Name: "alternate", Key: "alternate-key", Models: modelName}
		if tc.wantPlugin {
			alternate.Type = plugin.Type
			alternate.SetSetting(plugin.GetSetting())
		}
		if tc.shared {
			const alternateKey = "channel-route-ztest"
			alternateSource := taskProtocolPluginSource(alternateKey, "1.0.0", `["channel-route-model"]`, path, `return {model: ctx.model, requestBody: {normalized: true}};`)
			_, err := jsplugin.DefaultRegistry.Register(alternateSource, jsplugin.Options{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(alternateKey)) })
			alternate.SetSetting(kitdto.ChannelSettings{TaskPluginKey: alternateKey})
		}
		named["alternate"] = alternate
		channels = append(channels, alternate)
	}
	for _, channel := range channels {
		require.NoError(t, channel.Insert())
	}
	if tc.observationContext != "" {
		if tc.observationContext == "video" {
			for range 50 {
				service.RecordRouteObservation(plugin.Id, modelName, relayconstant.RelayModeVideoSubmit, false, true, 10000)
				service.RecordRouteObservation(ordinary.Id, modelName, relayconstant.RelayModeVideoSubmit, false, true, 100)
				service.RecordRouteObservation(plugin.Id, modelName, relayconstant.RelayModeUnknown, false, true, 100)
				service.RecordRouteObservation(ordinary.Id, modelName, relayconstant.RelayModeUnknown, false, true, 10000)
			}
		} else {
			for range 50 {
				service.RecordRouteObservation(plugin.Id, modelName, relayconstant.RelayModeResponses, true, true, 10000)
				service.RecordRouteObservation(ordinary.Id, modelName, relayconstant.RelayModeResponses, true, true, 100)
				service.RecordRouteObservation(plugin.Id, modelName, relayconstant.RelayModeResponses, false, true, 100)
				service.RecordRouteObservation(ordinary.Id, modelName, relayconstant.RelayModeResponses, false, true, 10000)
			}
		}
	}
	if tc.originPin {
		require.NoError(t, database.Create(&model.Task{TaskID: "task-route-origin", UserId: endpointRouteUserID, ChannelId: named["alternate"].Id, Platform: constant.TaskPlatform(key), Status: model.TaskStatusSuccess}).Error)
	}
	if len(tc.manual) > 0 {
		route := model.TokenRouting{TokenRouteProfile: model.TokenRouteProfile{Mode: model.TokenRouteManual}}
		allowed := map[int]bool{}
		for _, name := range tc.manual {
			id := named[name].Id
			route.Entries = append(route.Entries, model.TokenRouteEntry{ChannelID: id, Enabled: name != tc.disabledEntry})
			allowed[id] = true
		}
		_, err := model.SaveTokenRouting(context.Background(), endpointRouteUserID, endpointRouteUserID, route, allowed)
		require.NoError(t, err)
	}
	model.InitChannelCache()
	if tc.ordinaryBusy {
		permit, err := service.AcquireChannelConcurrency(context.Background(), ordinary)
		require.NoError(t, err)
		t.Cleanup(permit.Release)
	}
	if tc.pluginBusy {
		permit, err := service.AcquireChannelConcurrency(context.Background(), plugin)
		require.NoError(t, err)
		t.Cleanup(permit.Release)
	}
	if tc.userBusy {
		permit, err := service.AcquireUserConcurrency(context.Background(), endpointRouteUserID, 1)
		require.NoError(t, err)
		t.Cleanup(permit.Release)
	}
	serveEndpointRouteCase(t, tc, path, modelName, key, named, endpointRouteUserID)
	for name, channel := range named {
		want := 0
		if (tc.ordinaryBusy && name == "ordinary") || (tc.pluginBusy && name == "plugin") {
			want = 1
		}
		assert.Equal(t, want, service.ChannelInFlight(channel.Id), "channel %s permit must be released exactly once", name)
	}
}

func serveEndpointRouteCase(t *testing.T, tc endpointRouteCase, path, modelName, key string, named map[string]*model.Channel, requestUserID int) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"model":%q,"prompt":"a cat","size":"1024x1024","stream":%t}`, modelName, tc.stream))
	contentType := "application/json"
	if tc.multipart {
		var form bytes.Buffer
		writer := multipart.NewWriter(&form)
		require.NoError(t, writer.WriteField("model", modelName))
		require.NoError(t, writer.WriteField("prompt", "a cat"))
		file, err := writer.CreateFormFile("image", "reference.png")
		require.NoError(t, err)
		_, err = file.Write([]byte("image bytes"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		body, contentType = form.Bytes(), writer.FormDataContentType()
	}
	wantName := tc.want
	if wantName == "" {
		wantName = "ordinary"
		if tc.wantPlugin {
			wantName = "plugin"
		}
	}
	reached := false
	router := gin.New()
	router.POST(path, func(c *gin.Context) {
		c.Set("id", requestUserID)
		c.Set("user_max_concurrency", 1)
		common.SetContextKey(c, constant.ContextKeyTokenId, endpointRouteUserID)
		common.SetContextKey(c, constant.ContextKeyUserId, endpointRouteUserID)
		if tc.pinOrdinary || tc.pinPlugin {
			pinned := named["ordinary"]
			if tc.pinPlugin {
				pinned = named["plugin"]
			}
			service.GetChannelConstraints(c).AddPin(dto.ChannelPin{ChannelId: pinned.Id, Source: dto.PinSourceToken, Rank: dto.PinRankToken})
		}
	}, ModelRequestRateLimit(), RouteTaskPluginEndpoint(), PrepareTaskPluginEndpoint(), Distribute(), func(c *gin.Context) {
		reached = true
		if tc.observationContext == "stream" {
			assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyIsStream))
		}
		if tc.stream {
			assert.True(t, common.GetContextKeyBool(c, constant.ContextKeyIsStream), "stream context must survive plugin model resolution")
		}
		if tc.observationContext == "video" {
			assert.Equal(t, relayconstant.RelayModeVideoSubmit, c.GetInt("relay_mode"))
		}
		_, pinned := c.Get(jsplugin.ContextKeyPinnedEndpoint)
		assert.Equal(t, tc.wantPlugin, pinned)
		_, relayEndpoint := c.Get(jsplugin.ContextKeyRelayEndpoint)
		assert.Equal(t, !tc.wantPlugin, relayEndpoint)
		expected := named[wantName]
		if tc.wantPlugin {
			wantKey := key
			if tc.shared {
				wantKey = "channel-route-ztest"
			}
			assert.Equal(t, wantKey, c.GetString("task_plugin_key"))
			assert.Equal(t, map[string]any{"normalized": true}, c.MustGet("task_request"))
			if tc.mapped {
				assert.Equal(t, "channel-route-model", c.MustGet(jsplugin.ContextKeyPinnedEndpoint).(jsplugin.PinnedEndpoint).MappedModel)
			}
		} else {
			assert.Empty(t, c.GetString("expected_task_plugin_key"))
			assert.Empty(t, c.GetString("resolved_task_model"))
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			got, err := storage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, body, got, "ordinary relay must receive the original body")
		}
		assert.Equal(t, expected.Id, common.GetContextKeyInt(c, constant.ContextKeyChannelId))
		assert.Equal(t, expected.Key, common.GetContextKeyString(c, constant.ContextKeyChannelKey))
		assert.Equal(t, 1, service.ChannelInFlight(expected.Id), "the selected channel holds one permit while relaying")
		if tc.retry {
			service.AppendUsedChannel(c, expected.Id)
			next, selectErr := service.SelectChannelForRequest(c, modelName, &service.RetryParam{Ctx: c, ModelName: modelName, RequestPath: path, Retry: common.GetPointer(1)})
			require.Nil(t, selectErr)
			require.NotNil(t, next)
			assert.Equal(t, named["alternate"].Id, next.Id)
		}
		c.Status(http.StatusNoContent)
	})
	serve := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		request.Header.Set("Content-Type", contentType)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
	recorder := serve()
	status := tc.wantStatus
	if status == 0 {
		status = http.StatusNoContent
	}
	assert.Equal(t, status, recorder.Code, recorder.Body.String())
	assert.Equal(t, status == http.StatusNoContent, reached)
	if tc.decoderRejects && status == http.StatusBadRequest {
		assert.Contains(t, recorder.Body.String(), "plugin rejected this request")
	}
	if tc.pluginBusy && tc.wantErrorCode == "" {
		assert.Contains(t, recorder.Body.String(), "channel_concurrency_exceeded")
	}
	if tc.wantErrorCode != "" {
		assert.Contains(t, recorder.Body.String(), tc.wantErrorCode)
	}
	if !tc.userBusy {
		permit, err := service.AcquireUserConcurrency(context.Background(), requestUserID, 1)
		require.NoError(t, err, "user concurrency permit must be released when the request returns")
		permit.Release()
	}
}
