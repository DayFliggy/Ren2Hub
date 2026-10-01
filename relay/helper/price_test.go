package helper

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestImageExpressionDoesNotApplyLegacyPromptExtendAgain(t *testing.T) {
	c, info := pricingFixture(t, "image-test", model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `param("parameters.prompt_extend") == true ? tier("extended",fixed(0.02)) : tier("plain",fixed(0.01))`})
	info.ChannelMeta = &relaycommon.ChannelMeta{ApiType: constant.APITypeAli, UpstreamModelName: "z-image-turbo"}
	meta, err := FinalImagePriceMeta(c, info, strings.NewReader(`{"parameters":{"n":3,"prompt_extend":true}}`))
	require.NoError(t, err)
	price, err := ModelPriceHelper(c, info, 0, meta)
	require.NoError(t, err)
	assert.Equal(t, 10000, price.QuotaToPreConsume)
	assert.Empty(t, price.Quote.OtherRatios)
}

func pricingFixture(t *testing.T, name string, values model.PricingValues) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	t.Setenv("CRYPTO_SECRET", "pricing-helper-test-secret")
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.Option{}, &model.Channel{}, &model.Ability{}, &model.Model{}, &model.Vendor{}))
	previousOptions := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousOptions })
	previous := model.DB
	mainKind, logKind := common.MainDatabaseType(), common.LogDatabaseType()
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previous
		common.SetDatabaseTypes(mainKind, logKind)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	saveHelperPricing(t, name, values)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	c.Set("channel_ratio", "1")
	info := &relaycommon.RelayInfo{OriginModelName: name, BillingRequestInput: &billingexpr.RequestInput{Body: []byte("{}"), Attributes: map[string]any{}, FrozenAt: time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)}}
	return c, info
}
func saveHelperPricing(t *testing.T, name string, values model.PricingValues) {
	t.Helper()
	snapshot, err := model.GetModelPricingSnapshot([]string{name})
	require.NoError(t, err)
	version := snapshot.EmptyVersion
	for _, entry := range snapshot.Entries {
		if entry.ModelName == name {
			version = entry.Version
		}
	}
	err = model.UpdateModelPricing([]model.ModelPricingChange{{ModelName: name, ExpectedVersion: version, Pricing: values}})
	require.NoError(t, err)
}
func TestModelPricingEmptyAndExplicitZero(t *testing.T) {
	c, info := pricingFixture(t, "priced", model.PricingValues{"ModelPrice": "0"})
	price, err := ModelPriceHelper(c, info, 10, nil)
	require.NoError(t, err)
	assert.Zero(t, price.Quota)
	assert.NotNil(t, price.Usage.Evidence)
	info.OriginModelName = "missing"
	_, err = ModelPriceHelper(c, info, 10, nil)
	require.ErrorContains(t, err, "not configured")
}
func TestModelPricingEstimatesInputOnlyAndReplaysEncryptedProbe(t *testing.T) {
	c, info := pricingFixture(t, "model", model.PricingValues{"billing_setting.billing_mode": "tiered_expr", "billing_setting.billing_expr": `p * float(param(header("x-path")) ?? 1) + c * 8`})
	info.BillingRequestInput.Body = []byte(`{"rate":4,"private":"do-not-persist"}`)
	info.BillingRequestInput.Headers = map[string]string{"x-path": "rate", "authorization": "private-key"}
	price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 9000})
	require.NoError(t, err)
	assert.Equal(t, 200, price.Quota)
	assert.Zero(t, price.Usage.OutputTokens)
	replayed, err := model.ReplayBilling(price.Quote, *price.Usage, info.RequestId, info.BillingAttemptID, "estimate")
	require.NoError(t, err)
	assert.Equal(t, price.Charge, replayed)
	encoded, err := common.Marshal(price.Usage)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "private-key")
	assert.NotContains(t, string(encoded), "do-not-persist")
}
func TestModelPricingRetryPinsDefinitionAndAppliesChannelOnce(t *testing.T) {
	c, info := pricingFixture(t, "model", model.PricingValues{"ModelPrice": "0.002"})
	c.Set("channel_ratio", "1.5")
	first, err := ModelPriceHelper(c, info, 0, nil)
	require.NoError(t, err)
	require.NoError(t, PinBillingPrice(c, info))
	firstAttempt := info.BillingAttemptID
	saveHelperPricing(t, "model", model.PricingValues{"ModelPrice": "0.5"})
	second, err := ModelPriceHelper(c, info, 0, nil)
	require.NoError(t, err)
	assert.Equal(t, 1500, second.Quota)
	assert.Equal(t, first.Quote.PriceVersion, second.Quote.PriceVersion)
	assert.NotEqual(t, firstAttempt, info.BillingAttemptID)
	_, err = model.ReplayBilling(second.Quote, *second.Usage, info.RequestId, firstAttempt, "estimate")
	require.Error(t, err)
}
func TestModelPricingMultiplierNeverChangesSettlement(t *testing.T) {
	c, info := pricingFixture(t, "model", model.PricingValues{"ModelRatio": "2", "CompletionRatio": "3"})
	settings := operation_setting.GetQuotaSetting()
	previous := settings.PreConsumeMultiplier
	settings.PreConsumeMultiplier = 4
	t.Cleanup(func() { settings.PreConsumeMultiplier = previous })
	price, err := ModelPriceHelper(c, info, 100, nil)
	require.NoError(t, err)
	assert.Equal(t, 800, price.QuotaToPreConsume)
	assert.Equal(t, 200, price.Quota)
	usage := billing.Usage{InputTokens: 100, OutputTokens: 10, ContextTokens: 100, InputIncludesCache: true, Complete: true, Terminal: "completed", Source: "provider"}
	_, charge, err := model.EvaluateBilling(price.Quote, usage, *info.BillingRequestInput, info.RequestId, info.BillingAttemptID, "settlement")
	require.NoError(t, err)
	assert.EqualValues(t, 260, charge.FinalQuota)
}

func TestModelPricingFreezesUnbufferedFallbackAndCoversItWithReservation(t *testing.T) {
	for _, test := range []struct {
		name       string
		multiplier float64
		reserve    int
	}{
		{name: "buffered", multiplier: 2, reserve: 400},
		{name: "below one", multiplier: 0.5, reserve: 200},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, info := pricingFixture(t, "model", model.PricingValues{"ModelRatio": "2"})
			settings := operation_setting.GetQuotaSetting()
			previous := settings.PreConsumeMultiplier
			settings.PreConsumeMultiplier = test.multiplier
			t.Cleanup(func() { settings.PreConsumeMultiplier = previous })
			price, err := ModelPriceHelper(c, info, 100, nil)
			require.NoError(t, err)
			assert.Equal(t, test.reserve, price.QuotaToPreConsume)
			assert.Equal(t, 200, price.Quota)
			assert.EqualValues(t, 200, price.Charge.FinalQuota)
			assert.Equal(t, billing.UsageSourceFrozenEstimate, price.Usage.Source)
			replayed, err := model.ReplayBilling(price.Quote, *price.Usage, info.RequestId, info.BillingAttemptID, "estimate")
			require.NoError(t, err)
			assert.Equal(t, price.Charge, replayed)

			legacy := *price.Usage
			legacy.Source, legacy.Evidence = billing.UsageSourceRequestEstimate, nil
			legacy, oldCharge, err := model.EvaluateBilling(price.Quote, legacy, *info.BillingRequestInput, info.RequestId, info.BillingAttemptID, "estimate")
			require.NoError(t, err)
			oldReplay, err := model.ReplayBilling(price.Quote, legacy, info.RequestId, info.BillingAttemptID, "estimate")
			require.NoError(t, err)
			assert.Equal(t, oldCharge, oldReplay)
			assert.EqualValues(t, int(float64(200)*test.multiplier), oldCharge.FinalQuota)
		})
	}
}
func TestReadOnlyPriceQualificationCreatesNoEvidence(t *testing.T) {
	c, info := pricingFixture(t, "model", model.PricingValues{"ModelRatio": "1"})
	_, err := QualifyPublishedPrice(c, info)
	require.NoError(t, err)
	var count int64
	require.NoError(t, model.DB.WithContext(context.Background()).Model(&model.BillingPriceVersion{}).Count(&count).Error)
	assert.Zero(t, count)
}
