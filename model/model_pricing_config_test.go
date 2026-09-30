package model

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func modelPricingConfigDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Option{}, &Channel{}, &Ability{}, &Model{}, &Vendor{}))
	previous, previousRegistry := DB, jsplugin.DefaultRegistry
	previousType := common.MainDatabaseType()
	DB, jsplugin.DefaultRegistry = db, jsplugin.NewRegistry()
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	previousOptions := make(map[string]string)
	for _, key := range modelPricingOptionKeys {
		previousOptions[key] = common.OptionMap[key]
	}
	t.Cleanup(func() {
		for key, value := range previousOptions {
			if value == "" {
				value = "{}"
			}
			require.NoError(t, updateOptionMap(key, value))
		}
		DB, jsplugin.DefaultRegistry = previous, previousRegistry
		common.SetMainDatabaseType(previousType)
		require.NoError(t, connection.Close())
	})
	return db
}

func TestModelPricingConfigEmptyExplicitZeroAndExactDecimals(t *testing.T) {
	db := modelPricingConfigDB(t)
	snapshot, err := GetModelPricingSnapshot(nil)
	require.NoError(t, err)
	assert.Empty(t, snapshot.Entries)
	effective, err := PreviewModelPricing("gpt-5", PricingValues{})
	require.NoError(t, err)
	assert.Empty(t, effective)
	for _, tc := range []struct {
		name  string
		draft PricingValues
	}{
		{"token", PricingValues{"ModelRatio": "1.234567890123456789", "CompletionRatio": "0"}},
		{"request", PricingValues{"ModelPrice": "0"}},
		{"expression", PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `tier("base", p * 2 + c * 4)`}},
	} {
		require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: tc.name, ExpectedVersion: snapshot.EmptyVersion, Pricing: tc.draft}}))
	}
	var option Option
	require.NoError(t, db.Where(map[string]any{"key": "ModelRatio"}).First(&option).Error)
	assert.Contains(t, option.Value, `1.234567890123456789`)
	assert.NotContains(t, option.Value, `"1.234567890123456789"`)
	snapshot, err = GetModelPricingSnapshot([]string{"token", "request"})
	require.NoError(t, err)
	for _, entry := range snapshot.Entries {
		if entry.ModelName == "token" {
			assert.Equal(t, json.Number("1.234567890123456789"), entry.Configured["ModelRatio"])
			assert.Equal(t, "0", PricingValuesForAPI(entry.Effective)["CompletionRatio"])
		}
		if entry.ModelName == "request" {
			assert.Equal(t, json.Number("0"), entry.Effective["ModelPrice"])
		}
		require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: entry.ModelName, ExpectedVersion: entry.Version, Reset: true}}))
		preview, err := PreviewModelPricing(entry.ModelName, PricingValues{})
		require.NoError(t, err)
		assert.Empty(t, preview)
	}
}

func TestModelPricingConfigRejectsInvalidAmountsAndMissingDraft(t *testing.T) {
	modelPricingConfigDB(t)
	for _, value := range []any{"", "-1", "NaN", "Infinity", "1e999", " 1", nil, true, math.NaN(), math.Inf(1)} {
		assert.Error(t, ValidateModelPricing("model", PricingValues{"ModelPrice": value}))
	}
	assert.Error(t, ValidateModelPricing("model", PricingValues{"billing_setting.billing_mode": "tiered_expr"}))
	assert.Error(t, ValidateModelPricing("model", PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `tier("base", -1)`}))
	assert.Error(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "model", ExpectedVersion: ModelPricingVersion(PricingValues{})}}))
}

func TestModelPricingConfigRereadsAfterCreatingFirstOptionRows(t *testing.T) {
	db := modelPricingConfigDB(t)
	callback := "test:price-created-during-initialization"
	require.NoError(t, db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
		row, ok := tx.Statement.Dest.(*Option)
		if !ok || row.Key != billing_setting.PluginBillingExprOption {
			return
		}
		// Reproduces data becoming visible while namespace row initialization waits.
		err := tx.Session(&gorm.Session{NewDB: true}).Model(&Option{}).Where(map[string]any{"key": "ModelPrice"}).Update("value", `{"other-writer":0.4}`).Error
		if err != nil {
			tx.AddError(err)
		}
	}))
	t.Cleanup(func() { require.NoError(t, db.Callback().Create().Remove(callback)) })
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "my-model", ExpectedVersion: ModelPricingVersion(PricingValues{}), Pricing: PricingValues{"ModelPrice": "0.2"}}}))
	snapshot, err := GetModelPricingSnapshot(nil)
	require.NoError(t, err)
	assert.Len(t, snapshot.Entries, 2, "first writer must preserve the row seen after conflict handling")
}

func TestModelPricingConfigConcurrentVersionCheckIsAtomic(t *testing.T) {
	modelPricingConfigDB(t)
	errors := make(chan error, 2)
	var workers sync.WaitGroup
	for _, price := range []string{"1", "2"} {
		workers.Add(1)
		go func() {
			defer workers.Done()
			errors <- UpdateModelPricing([]ModelPricingChange{{ModelName: "race", ExpectedVersion: ModelPricingVersion(PricingValues{}), Pricing: PricingValues{"ModelPrice": price}}})
		}()
	}
	workers.Wait()
	close(errors)
	success, conflicts := 0, 0
	for err := range errors {
		if err == nil {
			success++
		} else {
			assert.ErrorIs(t, err, ErrModelPricingConflict)
			conflicts++
		}
	}
	assert.Equal(t, 1, success)
	assert.Equal(t, 1, conflicts)
}

func TestModelPricingConfigPluginOverridesAndStaleSchemas(t *testing.T) {
	modelPricingConfigDB(t)
	for _, key := range []string{"alpha", "beta"} {
		source := fmt.Sprintf(`export const meta={apiVersion:1,key:%q,name:%q,version:"1.0.0",author:{name:"Test"},models:["task-model"],fetchMode:"per_task",usageSchema:{seconds:{type:"number",unit:"second"}}};export function buildSubmitRequest(){return {}};export function parseSubmitResponse(){return {}};export function buildQueryRequest(){return {}};export function parseTaskResult(){return {}};`, key, key)
		_, err := jsplugin.DefaultRegistry.Register(source, jsplugin.Options{})
		require.NoError(t, err)
	}
	values := PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"alpha": `u("seconds") * 0.4`}}
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "task-model", ExpectedVersion: ModelPricingVersion(PricingValues{}), Pricing: values}}))
	snapshot, err := GetModelPricingSnapshot([]string{"task-model"})
	require.NoError(t, err)
	assert.Len(t, snapshot.Entries[0].PluginVariants, 2)
	require.NoError(t, jsplugin.DefaultRegistry.Unregister("alpha"))
	require.NoError(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "task-model", ExpectedVersion: snapshot.Entries[0].Version, Pricing: values}}))
	changed := PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"alpha": `u("seconds") * 0.8`}}
	assert.Error(t, UpdateModelPricing([]ModelPricingChange{{ModelName: "task-model", ExpectedVersion: snapshot.Entries[0].Version, Pricing: changed}}))
	assert.Error(t, ValidateModelPricing("task-model", PricingValues{billing_setting.PluginBillingExprOption: map[string]any{"beta": `u("undeclared") * 0.4`}}))
}

func TestModelPricingConfigConversionPreservesTokenAndImageCharges(t *testing.T) {
	modelPricingConfigDB(t)
	converted, err := PreviewModelPricingConversion("plain-model", PricingValues{"ModelRatio": "1.234567890123456789", "CompletionRatio": "2", "CacheRatio": "0"})
	require.NoError(t, err)
	assert.Contains(t, converted.Expression, "2.469135780246913578")
	assert.Contains(t, converted.Expression, "cr * 0")
	amount, _, err := billingexpr.RunExpr(converted.Expression, billingexpr.TokenParams{P: 1000, C: 1000, CR: 100})
	require.NoError(t, err)
	assert.InDelta(t, 7407.407340740741, amount, 1e-9)
	converted, err = PreviewModelPricingConversion("dall-e-3", PricingValues{"ModelPrice": "0.04"})
	require.NoError(t, err)
	n := 2
	amount, _, err = billingexpr.RunExprWithRequest(converted.Expression, billingexpr.TokenParams{}, billingexpr.RequestInput{Body: []byte(`{"size":"1024x1792","quality":"hd"}`), ImageCount: &n})
	require.NoError(t, err)
	assert.InDelta(t, 240000, amount, 1e-9)
}
