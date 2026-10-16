package plugins_test

import (
	"crypto/sha256"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"
	taskplugin "github.com/QuantumNous/new-api/relay/channel/task/jsplugin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestFactoryCompletionFactsPreserveInvalidAndZeroUsage(t *testing.T) {
	for _, factory := range []struct{ key, model, field, unit string }{
		{"kling", "kling-v1", "units", "credit"},
		{"vidu", "viduq2", "credits", "credit"},
		{"sora", "sora-2", "seconds", "second"},
	} {
		t.Run(factory.key, func(t *testing.T) {
			source, err := builtinplugins.Source(factory.key)
			require.NoError(t, err)
			plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
			require.NoError(t, err)
			maximum := common.MaxQuota
			if factory.unit == "second" {
				maximum = relaycommon.MaxTaskDurationSeconds
			}
			definition, err := billing.NewModelPriceVersion(factory.model, billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": fmt.Sprintf("u(%q) * 0.4", factory.field)}, true)
			require.NoError(t, err)
			definition.UsageSchema = &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{factory.field: {Type: "number", Unit: factory.unit, Source: billingexpr.UsageCompletionFact, Required: true, Minimum: "0", Maximum: fmt.Sprint(maximum)}}}
			quote, err := billing.NewQuote(definition, "1", nil, time.Now())
			require.NoError(t, err)
			for _, test := range []struct {
				name    string
				value   any
				wantUSD string
			}{
				{"number", 10, "4"}, {"decimal_string", "10", "4"},
				{"fraction", 3.5, "1.4"}, {"fraction_string", "3.5", "1.4"},
				{"exponent", "1e1", "4"}, {"zero", 0, "0"}, {"zero_string", "0", "0"},
				{"missing", nil, ""}, {"null", nil, ""},
				{"blank", "", ""}, {"whitespace", " \t ", ""},
				{"false", false, ""}, {"true", true, ""},
				{"empty_array", []any{}, ""}, {"array", []any{10}, ""},
				{"object", map[string]any{"value": 10}, ""},
				{"numeric_prefix", "10INVALID", ""}, {"non_decimal", "0x10", ""},
				{"negative", -1, ""}, {"negative_string", "-1", ""},
				{"overflow", maximum + 1, ""}, {"non_finite", "1e400", ""},
			} {
				t.Run(test.name, func(t *testing.T) {
					body := map[string]any{"state": "success", "status": "completed"}
					container, field := body, factory.field
					if factory.key == "kling" {
						container, field = map[string]any{"task_status": "succeed"}, "final_unit_deduction"
						body["data"] = container
					}
					if test.name != "missing" {
						container[field] = test.value
					}
					encoded, err := common.Marshal(body)
					require.NoError(t, err)
					adaptor := taskplugin.New(plugin)
					result, err := adaptor.ParseTaskResultForTask(&model.Task{Properties: model.Properties{OriginModelName: factory.model, UpstreamModelName: factory.model}}, nil, encoded)
					require.NoError(t, err)
					assert.Equal(t, "SUCCESS", result.Status, "the provider result stays successful even when its facts cannot be priced")
					charge, chargeErr := evaluateFactoryCharge(t, quote, result.UsageFacts)
					if test.wantUSD == "" {
						if test.name != "missing" {
							assert.NotEmpty(t, result.UsageError, "malformed facts cannot be reported as confirmed zero")
						}
						assert.Empty(t, result.UsageFacts)
						assert.Error(t, chargeErr)
						return
					}
					assert.Empty(t, result.UsageError)
					require.NoError(t, chargeErr)
					assert.EqualValues(t, decimal.RequireFromString(test.wantUSD).Mul(decimal.NewFromInt(500000)).IntPart(), charge.FinalQuota)
					assert.Contains(t, result.UsageFacts, factory.field, "explicit zero remains a present fact")
				})
			}
		})
	}
}

func TestSoraCompletionSecondsPresencePrecedesDuration(t *testing.T) {
	source, err := builtinplugins.Source("sora")
	require.NoError(t, err)
	plugin, err := jsplugin.CompilePlugin(source, jsplugin.Options{})
	require.NoError(t, err)
	for _, test := range []struct {
		name, body string
		want       map[string]any
		invalid    bool
	}{
		{"explicit_zero", `{"status":"completed","seconds":0,"duration":10}`, map[string]any{"seconds": "0"}, false},
		{"fallback_if_absent", `{"status":"completed","duration":10}`, map[string]any{"seconds": "10"}, false},
		{"null_is_not_absent", `{"status":"completed","seconds":null,"duration":10}`, nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := taskplugin.New(plugin).ParseTaskResultForTask(&model.Task{}, nil, []byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, "SUCCESS", result.Status)
			assert.Equal(t, test.want, result.UsageFacts)
			assert.Equal(t, test.invalid, result.UsageError != "")
		})
	}
}

func TestHailuoUsageFactsMatchEveryModelProfile(t *testing.T) {
	plugin := loadHailuoPlugin(t)
	for _, name := range plugin.Meta.Models {
		t.Run(name, func(t *testing.T) {
			schema, _ := plugin.Meta.UsageForModel(name)
			facts := callHailuoHook(t, plugin, "extractUsage", map[string]any{"upstreamModel": name, "requestBody": map[string]any{"prompt": "p"}})
			assert.Equal(t, slices.Sorted(maps.Keys(schema)), slices.Sorted(maps.Keys(facts)))
			if name == "MiniMax-H3" {
				return
			}
			result, err := taskplugin.New(plugin).ParseTaskResultForTask(&model.Task{Properties: model.Properties{OriginModelName: name, UpstreamModelName: name}}, nil,
				[]byte(`{"status":"Success","video_width":1366,"video_height":768}`))
			require.NoError(t, err)
			assert.Empty(t, result.UsageError)
			for key := range result.UsageFacts {
				assert.Contains(t, schema, key, "completion must also obey the selected model profile")
			}
		})
	}
}

func factoryPriceDatabase(t *testing.T) {
	t.Helper()
	t.Setenv("CRYPTO_SECRET", "factory-pricing-test-secret")
	previousOptions, previousRegistry := common.OptionMap, jsplugin.DefaultRegistry
	common.OptionMap = map[string]string{}
	jsplugin.DefaultRegistry = jsplugin.NewRegistry()
	t.Cleanup(func() { common.OptionMap = previousOptions; jsplugin.DefaultRegistry = previousRegistry })
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.Option{}, &model.Channel{}, &model.Ability{}, &model.TaskPluginNamespace{}, &model.TaskPlugin{}))
	previousDB := model.DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousMain, previousLog)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
}

func TestHailuoPublishedResolutionPriceReachesPrecharge(t *testing.T) {
	factoryPriceDatabase(t)
	plugin := loadHailuoPlugin(t)
	_, err := jsplugin.DefaultRegistry.RegisterFactory(plugin.Source, jsplugin.Options{Key: "hailuo"})
	require.NoError(t, err)
	name := "MiniMax-Hailuo-2.3"
	values := model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("resolution") == "768P" ? 1 : 2`}
	require.NoError(t, model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: name, ExpectedVersion: model.ModelPricingVersion(model.PricingValues{}), Pricing: values}}))
	price, err := model.ResolveModelBillingPrice(t.Context(), name, "hailuo")
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("channel_ratio", "1")
	c.Set("task_request", map[string]any{"model": price.Model, "prompt": "p", "duration": 10, "resolution": "768P"})
	info := &relaycommon.RelayInfo{OriginModelName: price.Model, TaskPluginRef: &model.TaskPluginRef{Key: "hailuo", Version: plugin.Meta.Version, SourceHash: plugin.SourceHash},
		ChannelMeta:         &relaycommon.ChannelMeta{UpstreamModelName: price.Model, ApiKey: "test-key", ChannelBaseUrl: "https://provider.example"},
		TaskRelayInfo:       &relaycommon.TaskRelayInfo{PublicTaskID: "profile-price-test"},
		BillingRequestInput: &billingexpr.RequestInput{Attributes: map[string]any{}, FrozenAt: time.Now()}}
	adaptor := taskplugin.New(plugin)
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	facts, err := adaptor.ExtractUsageFactsValidated(c, info)
	require.NoError(t, err)
	prepared, err := helper.ModelPriceHelperWithTaskFacts(c, info, facts)
	require.NoError(t, err)
	assert.Equal(t, 500000, prepared.QuotaToPreConsume)
	result, err := adaptor.ParseTaskResultForTask(&model.Task{Properties: model.Properties{OriginModelName: price.Model, UpstreamModelName: price.Model}}, nil,
		[]byte(`{"status":"Success","video_width":1366,"video_height":768}`))
	require.NoError(t, err)
	charge, err := evaluateFactoryCharge(t, prepared.Quote, result.UsageFacts)
	require.NoError(t, err)
	assert.Equal(t, "1", charge.TotalUSD)
}

func TestHailuoH3InvalidCompletionFactsRetainTerminalResult(t *testing.T) {
	plugin := loadHailuoPlugin(t)
	for _, test := range []struct {
		name  string
		value any
	}{
		{"null", nil}, {"blank", ""}, {"bool", false}, {"array", []any{0}},
		{"numeric_prefix", "0INVALID"}, {"negative", -1}, {"fractional_count", 1.5},
		{"out_of_range", 10}, {"non_finite", "1e400"},
	} {
		t.Run(test.name, func(t *testing.T) {
			body, err := common.Marshal(map[string]any{"task": map[string]any{
				"status": "succeeded", "resolution": "768P",
				"usage": map[string]any{"output_seconds": 5, "input_seconds": 0, "input_image_count": test.value},
			}})
			require.NoError(t, err)
			result, err := taskplugin.New(plugin).ParseTaskResultForTask(&model.Task{Properties: model.Properties{UpstreamModelName: "MiniMax-H3"}}, nil, body)
			require.NoError(t, err)
			assert.Equal(t, "SUCCESS", result.Status)
			assert.NotEmpty(t, result.UsageError)
			assert.Empty(t, result.UsageFacts)
		})
	}
}

func TestHailuoUpgradeKeepsPinnedLegacySourceAndQuote(t *testing.T) {
	factoryPriceDatabase(t)
	// This saved version models the legacy union contract. Exact loading must
	// not replace its executable or quote with the narrower current factory.
	const legacySource = `export const meta={apiVersion:1,key:"hailuo",name:"Legacy Hailuo",version:"1.2.0",author:{name:"Test"},models:["T2V-01"],fetchMode:"per_task",usageSchema:{resolution:{enum:["720P"]},input_images:{type:"number",unit:"count"}}};
export function buildSubmitRequest(ctx){return {url:ctx.baseUrl+"/submit"};}
export function parseSubmitResponse(ctx,resp){return {taskId:resp.body.task_id};}
export function buildQueryRequest(ctx){return {url:ctx.baseUrl+"/query"};}
export function parseTaskResult(){return {status:"SUCCESS"};}
export function extractUsageOnComplete(ctx,result,body){return {resolution:body.resolution,input_images:body.input_images};}`
	legacy := model.TaskPlugin{Key: "hailuo", Version: "1.2.0", Factory: true,
		Source: legacySource, SourceHash: fmt.Sprintf("%x", sha256.Sum256([]byte(legacySource)))}
	require.NoError(t, model.SaveTaskPlugin(&legacy))
	currentSource, err := builtinplugins.Source("hailuo")
	require.NoError(t, err)
	current := loadHailuoPlugin(t)
	require.NotEqual(t, legacy.Version, current.Meta.Version, "changed source needs a distinct immutable version")
	updated := model.TaskPlugin{Key: "hailuo", Version: current.Meta.Version, Factory: true,
		Source: model.LongText(currentSource), SourceHash: current.SourceHash}
	require.NoError(t, model.SaveTaskPlugin(&updated))
	definition, err := billing.NewModelPriceVersion("T2V-01", billing.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `u("resolution") == "720P" ? 1 + u("input_images") * 0.2 : 2`}, true)
	require.NoError(t, err)
	definition.UsageSchema = &billingexpr.UsageSchema{Version: 1, Fields: map[string]billingexpr.UsageField{
		"resolution":   {Type: "string", Source: billingexpr.UsageCompletionFact, Enum: []string{"720P"}, Required: true},
		"input_images": {Type: "number", Unit: "count", Source: billingexpr.UsageCompletionFact, Minimum: "0", Maximum: "128", Required: true},
	}}
	price, err := model.StoreBillingPriceEvidence(t.Context(), definition)
	require.NoError(t, err)
	quote, err := billing.NewQuote(*price, "1", nil, time.Now())
	require.NoError(t, err)
	quoteHash, schemaHash := quote.Hash, quote.PriceVersion.UsageSchema.Hash
	task := &model.Task{Properties: model.Properties{OriginModelName: "T2V-01", UpstreamModelName: "T2V-01"},
		PrivateData: model.TaskPrivateData{PluginRef: &model.TaskPluginRef{Key: legacy.Key, Version: legacy.Version, SourceHash: legacy.SourceHash}}}
	pinned, err := service.LoadTaskPluginForTask(task)
	require.NoError(t, err)
	assert.Equal(t, legacy.SourceHash, pinned.SourceHash)
	result, err := taskplugin.New(pinned).ParseTaskResultForTask(task, nil, []byte(`{"resolution":"720P","input_images":0}`))
	require.NoError(t, err)
	charge, err := evaluateFactoryCharge(t, quote, result.UsageFacts)
	require.NoError(t, err)
	assert.Equal(t, "1", charge.TotalUSD)
	assert.Equal(t, quoteHash, quote.Hash)
	assert.Equal(t, schemaHash, quote.PriceVersion.UsageSchema.Hash)
	require.NoError(t, model.ValidatePublishedBillingQuote(t.Context(), quote))
}

func evaluateFactoryCharge(t *testing.T, quote *billing.Quote, facts map[string]any) (*billing.ChargeResult, error) {
	t.Helper()
	values, err := service.TaskExpressionUsage(facts, quote.PriceVersion.UsageSchema)
	if err != nil {
		return nil, err
	}
	cipher, err := billing.NewEvidenceCipher([]byte("factory-charge-test"))
	require.NoError(t, err)
	_, charge, err := cipher.Evaluate(quote, billing.Usage{Complete: true, Source: "provider", Terminal: "completed", ActualFacts: facts}, billingexpr.RequestInput{Usage: values}, billing.EvidenceBinding{RequestID: "factory-request", AttemptID: "factory-attempt", ConfigHash: billing.PricingConfigHash(quote), Phase: "settlement"})
	return charge, err
}
