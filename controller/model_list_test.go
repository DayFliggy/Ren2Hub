package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/jsplugin"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type listModelsResponse struct {
	Success bool               `json:"success"`
	Data    []dto.OpenAIModels `json:"data"`
	Object  string             `json:"object"`
}

type userModelsResponse struct {
	Success bool     `json:"success"`
	Data    []string `json:"data"`
}

func setupModelListControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	t.Setenv("CRYPTO_SECRET", "controller-pricing-test-evidence-key")
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis := common.RedisEnabled
	common.OptionMapRWMutex.Lock()
	previousOptions := common.OptionMap
	common.OptionMap = make(map[string]string)
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previousOptions
		common.OptionMapRWMutex.Unlock()
	})

	initModelListColumnNames(t)

	gin.SetMode(gin.TestMode)
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	model.DB = db
	model.LOG_DB = db

	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}, &model.BillingPriceVersion{}, &model.Option{}))
	require.NoError(t, db.AutoMigrate(&model.UserChannelEntitlement{}, &model.UserRouteProfile{}, &model.UserRouteGroup{}, &model.UserRouteEntry{}))

	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})

	return db
}

func initModelListColumnNames(t *testing.T) {
	t.Helper()

	originalIsMasterNode := common.IsMasterNode
	originalSQLitePath := common.SQLitePath
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalSQLDSN, hadSQLDSN := os.LookupEnv("SQL_DSN")
	defer func() {
		common.IsMasterNode = originalIsMasterNode
		common.SQLitePath = originalSQLitePath
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		if hadSQLDSN {
			require.NoError(t, os.Setenv("SQL_DSN", originalSQLDSN))
		} else {
			require.NoError(t, os.Unsetenv("SQL_DSN"))
		}
	}()

	common.IsMasterNode = false
	common.SQLitePath = fmt.Sprintf("file:%s_init?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, os.Setenv("SQL_DSN", "local"))

	// This fixture owns a disposable database and supplies the release marker.
	bootstrap, bootstrapErr := gorm.Open(sqlite.Open(common.SQLitePath), &gorm.Config{})
	require.NoError(t, bootstrapErr)
	require.NoError(t, bootstrap.AutoMigrate(&model.BillingSchema{}))
	cipher, err := billing.NewEvidenceCipher([]byte(os.Getenv("CRYPTO_SECRET")))
	require.NoError(t, err)
	require.NoError(t, bootstrap.Create(&model.BillingSchema{ID: 1, Version: model.BillingSchemaVersion, EvidenceKeyID: cipher.KeyID()}).Error)
	bootstrapSQL, bootstrapErr := bootstrap.DB()
	require.NoError(t, bootstrapErr)
	defer bootstrapSQL.Close()
	require.NoError(t, model.InitDB())
	if model.DB != nil {
		sqlDB, err := model.DB.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	}
}

func withTieredBillingConfig(t *testing.T, modes map[string]string, exprs map[string]string) {
	t.Helper()

	for name, mode := range modes {
		require.Equal(t, "tiered_expr", mode)
		snapshot, err := model.GetModelPricingSnapshot([]string{name})
		require.NoError(t, err)
		err = model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: name, ExpectedVersion: snapshot.Entries[0].Version, Pricing: model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": exprs[name]}}})
		if strings.TrimSpace(exprs[name]) == "" {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
	model.InvalidatePricingCache()
}

func withSelfUseModeDisabled(t *testing.T) {
	t.Helper()

	original := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = false
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = original
	})
}

func withSelfUseModeEnabled(t *testing.T) {
	t.Helper()

	original := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = true
	t.Cleanup(func() {
		operation_setting.SelfUseModeEnabled = original
	})
}

func decodeListModelsPayload(t *testing.T, recorder *httptest.ResponseRecorder) listModelsResponse {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload listModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "list", payload.Object)
	return payload
}

func decodeListModelsResponse(t *testing.T, recorder *httptest.ResponseRecorder) map[string]struct{} {
	t.Helper()

	payload := decodeListModelsPayload(t, recorder)
	ids := make(map[string]struct{}, len(payload.Data))
	for _, item := range payload.Data {
		ids[item.Id] = struct{}{}
	}
	return ids
}

func pricingByModelName(pricings []model.Pricing) map[string]model.Pricing {
	byName := make(map[string]model.Pricing, len(pricings))
	for _, pricing := range pricings {
		byName[pricing.ModelName] = pricing
	}
	return byName
}

func decodeUserModelsResponse(t *testing.T, recorder *httptest.ResponseRecorder) []string {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload userModelsResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload.Data
}

func TestGetUserModelsIgnoresRetiredGroupQuery(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}).Error)
	require.NoError(t, db.Create(&model.User{
		Id:       1002,
		Username: "playground-model-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-default-only-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-disabled-model", ChannelId: 1, Enabled: false},
	}).Error)

	defaultRecorder := httptest.NewRecorder()
	defaultContext, _ := gin.CreateTestContext(defaultRecorder)
	defaultContext.Request = httptest.NewRequest(http.MethodGet, "/api/user/models?group=default", nil)
	defaultContext.Set("id", 1002)

	GetUserModels(defaultContext)

	defaultModels := decodeUserModelsResponse(t, defaultRecorder)
	require.ElementsMatch(t, []string{"zz-default-only-model"}, defaultModels)

	vipRecorder := httptest.NewRecorder()
	vipContext, _ := gin.CreateTestContext(vipRecorder)
	vipContext.Request = httptest.NewRequest(http.MethodGet, "/api/user/models?group=vip", nil)
	vipContext.Set("id", 1002)

	GetUserModels(vipContext)

	require.ElementsMatch(t, defaultModels, decodeUserModelsResponse(t, vipRecorder))
}

func TestGetUserModelsDoesNotAddCompactPermissionVariant(t *testing.T) {
	withSelfUseModeEnabled(t)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1004,
		Username: "compact-permission-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:     704,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-openai",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-zz-compact-capable,gemini-zz-compact-capable",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-zz-compact-capable",
		ChannelId: 704,
		Enabled:   true,
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gemini-zz-compact-capable",
		ChannelId: 704,
		Enabled:   true,
	}).Error)
	model.InvalidatePricingCache()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/user/models?group=default", nil)
	ctx.Set("id", 1004)
	GetUserModels(ctx)

	models := decodeUserModelsResponse(t, recorder)
	require.ElementsMatch(t, []string{
		"gpt-zz-compact-capable",
		"gemini-zz-compact-capable",
	}, models)

	standardRecorder := httptest.NewRecorder()
	standardCtx, _ := gin.CreateTestContext(standardRecorder)
	standardCtx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	standardCtx.Set("id", 1004)
	ListModels(standardCtx, constant.ChannelTypeOpenAI)
	require.Equal(t, map[string]struct{}{
		"gpt-zz-compact-capable":    {},
		"gemini-zz-compact-capable": {},
	}, decodeListModelsResponse(t, standardRecorder))
}

func TestListModelsUsesSamePermissionForCompactEndpoint(t *testing.T) {
	withSelfUseModeEnabled(t)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id:     705,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-token-projection",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "gpt-zz-compact-token-model",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gpt-zz-compact-token-model",
		ChannelId: 705,
		Enabled:   true,
	}).Error)
	model.InitChannelCache()
	model.InvalidatePricingCache()
	model.GetPricing()

	tests := []struct {
		name      string
		limit     map[string]bool
		endpoints []constant.EndpointType
		visible   bool
	}{
		{
			name:  "base only",
			limit: map[string]bool{"gpt-zz-compact-token-model": true},
			endpoints: []constant.EndpointType{
				constant.EndpointTypeOpenAI,
				constant.EndpointTypeOpenAIResponse,
				constant.EndpointTypeOpenAIResponseCompact,
			},
			visible: true,
		},
		{
			name:      "compact only",
			limit:     map[string]bool{"gpt-zz-compact-token-model-openai-compact": true},
			endpoints: nil,
			visible:   false,
		},
		{
			name: "base and compact",
			limit: map[string]bool{
				"gpt-zz-compact-token-model":                true,
				"gpt-zz-compact-token-model-openai-compact": true,
			},
			endpoints: []constant.EndpointType{
				constant.EndpointTypeOpenAI,
				constant.EndpointTypeOpenAIResponse,
				constant.EndpointTypeOpenAIResponseCompact,
			},
			visible: true,
		},
		{name: "no permission", limit: map[string]bool{}, visible: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
			common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
			common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, test.limit)

			ListModels(ctx, constant.ChannelTypeOpenAI)
			payload := decodeListModelsPayload(t, recorder)
			if !test.visible {
				require.Empty(t, payload.Data)
				return
			}
			require.Len(t, payload.Data, 1)
			require.Equal(t, "gpt-zz-compact-token-model", payload.Data[0].Id)
			require.Equal(t, test.endpoints, payload.Data[0].SupportedEndpointTypes)
		})
	}
}

func TestTokenModelPermissionTreatsCompactSuffixAsLiteral(t *testing.T) {
	base := "ordinary-model"
	literal := base + "-openai-compact"
	require.True(t, tokenAllowsModel(map[string]bool{base: true}, base))
	require.False(t, tokenAllowsModel(map[string]bool{base: true}, literal))
	require.True(t, tokenAllowsModel(map[string]bool{literal: true}, literal))
	require.False(t, tokenAllowsModel(map[string]bool{literal: true}, base))
}

func TestListModelsDoesNotProjectCompactPermissionIntoAnthropicList(t *testing.T) {
	withSelfUseModeEnabled(t)
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id:     706,
		Type:   constant.ChannelTypeOpenAI,
		Name:   "compact-anthropic-projection",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
		Models: "zz-compact-anthropic-model",
	}).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "zz-compact-anthropic-model",
		ChannelId: 706,
		Enabled:   true,
	}).Error)
	model.InitChannelCache()
	model.InvalidatePricingCache()
	model.GetPricing()

	list := func(limit map[string]bool) []dto.AnthropicModel {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
		common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, limit)
		ListModels(ctx, constant.ChannelTypeAnthropic)

		var payload struct {
			Data []dto.AnthropicModel `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
		return payload.Data
	}

	require.Empty(t, list(map[string]bool{
		"zz-compact-anthropic-model-openai-compact": true,
	}))
	models := list(map[string]bool{"zz-compact-anthropic-model": true})
	require.Len(t, models, 1)
	require.Equal(t, "zz-compact-anthropic-model", models[0].ID)
}

func TestListModelsIncludesTieredBillingModel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-tiered-visible-model":      "tiered_expr",
		"zz-tiered-empty-expr-model":   "tiered_expr",
		"zz-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-tiered-empty-expr-model": "   ",
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1001,
		Username: "model-list-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{Id: 1, Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-tiered-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-empty-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-tiered-missing-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-unpriced-model", ChannelId: 1, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1001)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-tiered-visible-model")
	require.NotContains(t, ids, "zz-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-unpriced-model")

	pricingByName := pricingByModelName(model.GetPricing())
	visiblePricing, ok := pricingByName["zz-tiered-visible-model"]
	require.True(t, ok)
	require.Equal(t, "tiered_expr", visiblePricing.BillingMode)
	require.NotEmpty(t, visiblePricing.BillingExpr)

	emptyExprPricing, ok := pricingByName["zz-tiered-empty-expr-model"]
	require.True(t, ok)
	require.Equal(t, "unconfigured", emptyExprPricing.BillingMode)
	require.Empty(t, emptyExprPricing.BillingExpr)

	missingExprPricing, ok := pricingByName["zz-tiered-missing-expr-model"]
	require.True(t, ok)
	require.Equal(t, "unconfigured", missingExprPricing.BillingMode)
	require.Empty(t, missingExprPricing.BillingExpr)
}

func TestListModelsIncludesPluginPricedTaskModel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withSelfUseModeDisabled(t)
	const name = "zz-plugin-priced-task-model"
	const alias = "zz-plugin-priced-task-alias"
	const aliasOnlyModel = "zz-plugin-alias-priced-task-model"
	const aliasOnly = "zz-plugin-alias-priced-task-alias"
	const key = "model-list-price-plugin"
	source := `export const meta={apiVersion:1,key:"model-list-price-plugin",name:"Model List Price",version:"1.0.0",author:{name:"Test"},models:["zz-plugin-priced-task-model","zz-plugin-alias-priced-task-model"],fetchMode:"per_task",usageSchema:{count:{type:"number",unit:"count"}}};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};`
	_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, jsplugin.DefaultRegistry.Unregister(key)) })
	require.NoError(t, db.Create(&model.User{Id: 1011, Username: "plugin-priced-task-user", Password: "password", Group: "default", Status: common.UserStatusEnabled}).Error)
	mapping := `{"zz-plugin-priced-task-alias":"zz-plugin-priced-task-model","zz-plugin-alias-priced-task-alias":"zz-plugin-alias-priced-task-model"}`
	require.NoError(t, db.Create(&model.Channel{Id: 711, Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI, Models: name + "," + alias + "," + aliasOnly + "," + aliasOnlyModel, ModelMapping: &mapping}).Error)
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{
		{
			ModelName: name, ExpectedVersion: model.ModelPricingVersion(model.PricingValues{}),
			Pricing: model.PricingValues{billing_setting.PluginBillingExprOption: map[string]any{key: `u("count") * 0.02`}},
		},
		{
			ModelName: aliasOnly, ExpectedVersion: model.ModelPricingVersion(model.PricingValues{}),
			Pricing: model.PricingValues{"billing_setting.billing_mode": billing_setting.BillingModeTieredExpr, "billing_setting.billing_expr": `u("count") * 0.03`},
		},
	}))
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: name, ChannelId: 711, Enabled: true},
		{Group: "default", Model: alias, ChannelId: 711, Enabled: true},
		{Group: "default", Model: aliasOnly, ChannelId: 711, Enabled: true},
		{Group: "default", Model: aliasOnlyModel, ChannelId: 711, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1011)
	ListModels(ctx, constant.ChannelTypeOpenAI)
	listed := decodeListModelsResponse(t, recorder)
	require.Contains(t, listed, name)
	require.Contains(t, listed, alias)
	require.Contains(t, listed, aliasOnly)
	require.NotContains(t, listed, aliasOnlyModel)
}

func TestListModelsUsesAdvancedCustomEndpointTypesFromPricingCache(t *testing.T) {
	withSelfUseModeEnabled(t)
	db := setupModelListControllerTestDB(t)

	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
		model.InvalidatePricingCache()
	})

	require.NoError(t, db.Create(&model.User{
		Id:       1003,
		Username: "advanced-custom-model-list-user",
		Password: "password",
		Group:    "default",
		Status:   common.UserStatusEnabled,
	}).Error)

	channel := &model.Channel{
		Id:     701,
		Type:   constant.ChannelTypeAdvancedCustom,
		Key:    "advanced-custom-key",
		Status: common.ChannelStatusEnabled,
		Name:   "advanced-custom-channel",
		Group:  "default",
		Models: "gemini-3.5-flash",
	}
	channel.SetOtherSettings(dto.ChannelOtherSettings{
		AdvancedCustom: &dto.AdvancedCustomConfig{
			Routes: []dto.AdvancedCustomRoute{
				{
					IncomingPath: "/v1/chat/completions",
					UpstreamPath: "/v1/chat/completions",
				},
				{
					IncomingPath: "/v1/responses",
					UpstreamPath: "/v1beta/models/{model}:generateContent",
					Converter:    "openai_responses_to_gemini_generate_content",
					Models:       []string{"re:^gemini-"},
				},
			},
		},
	})
	require.NoError(t, db.Create(channel).Error)
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "gemini-3.5-flash",
		ChannelId: 701,
		Enabled:   true,
	}).Error)

	model.InitChannelCache()
	model.GetPricing()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	ctx.Set("id", 1003)

	ListModels(ctx, constant.ChannelTypeOpenAI)

	payload := decodeListModelsPayload(t, recorder)
	require.Len(t, payload.Data, 1)
	require.Equal(t, "gemini-3.5-flash", payload.Data[0].Id)
	require.Equal(t, []constant.EndpointType{
		constant.EndpointTypeOpenAI,
		constant.EndpointTypeOpenAIResponse,
	}, payload.Data[0].SupportedEndpointTypes)
}

func TestListModelsTokenLimitIncludesTieredBillingModel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	withSelfUseModeDisabled(t)
	withTieredBillingConfig(t, map[string]string{
		"zz-token-tiered-visible-model":      "tiered_expr",
		"zz-token-tiered-empty-expr-model":   "tiered_expr",
		"zz-token-tiered-missing-expr-model": "tiered_expr",
	}, map[string]string{
		"zz-token-tiered-visible-model":    `tier("base", p * 1 + c * 2)`,
		"zz-token-tiered-empty-expr-model": "",
	})
	require.NoError(t, db.Create(&model.Channel{Id: 1, Status: common.ChannelStatusEnabled, Type: constant.ChannelTypeOpenAI}).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "zz-token-tiered-visible-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-token-tiered-empty-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-token-tiered-missing-expr-model", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "zz-token-unpriced-model", ChannelId: 1, Enabled: true},
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimitEnabled, true)
	common.SetContextKey(ctx, constant.ContextKeyTokenModelLimit, map[string]bool{
		"zz-token-tiered-visible-model":      true,
		"zz-token-tiered-empty-expr-model":   true,
		"zz-token-tiered-missing-expr-model": true,
		"zz-token-unpriced-model":            true,
	})

	ListModels(ctx, constant.ChannelTypeOpenAI)

	ids := decodeListModelsResponse(t, recorder)
	require.Contains(t, ids, "zz-token-tiered-visible-model")
	require.NotContains(t, ids, "zz-token-tiered-empty-expr-model")
	require.NotContains(t, ids, "zz-token-tiered-missing-expr-model")
	require.NotContains(t, ids, "zz-token-unpriced-model")
}

func TestCheckUpdatePasswordRequiresCurrentPassword(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	hashedPassword, err := common.Password2Hash("CurrentPassword123")
	require.NoError(t, err)
	user := &model.User{
		Username: "password-user",
		Password: hashedPassword,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	updatePassword, err := checkUpdatePassword("", "", user.Id)
	require.NoError(t, err)
	assert.False(t, updatePassword)

	updatePassword, err = checkUpdatePassword("", "NewPassword123", user.Id)
	require.Error(t, err)
	assert.False(t, updatePassword)
	assert.ErrorIs(t, err, errOriginalPasswordFail)

	updatePassword, err = checkUpdatePassword("CurrentPassword123", "NewPassword123", user.Id)
	require.NoError(t, err)
	assert.True(t, updatePassword)
}

func TestCheckUpdatePasswordRejectsHistoricalEmptyPassword(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	user := &model.User{
		Username: "legacy-passwordless-user",
		Password: "",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, db.Create(user).Error)

	updatePassword, err := checkUpdatePassword("", "NewPassword123", user.Id)
	require.Error(t, err)
	assert.False(t, updatePassword)
	assert.ErrorIs(t, err, errUserPasswordUnset)
}

func TestSetupLoginDoesNotTouchPasswordWhenPasswordFieldOmitted(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}, &model.UserSession{}))

	hashedPassword, err := common.Password2Hash("CurrentPassword123")
	require.NoError(t, err)
	user := &model.User{
		Username: "twofa-user",
		Password: hashedPassword,
		Role:     common.RoleCommonUser,
		Status:   common.UserStatusEnabled,
		Group:    "default",
	}
	require.NoError(t, db.Create(user).Error)

	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		setupLogin(&model.User{
			Id:       user.Id,
			Username: user.Username,
			Role:     user.Role,
			Status:   user.Status,
			Group:    user.Group,
		}, nil, c)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	assert.Equal(t, hashedPassword, stored.Password)
}
