package helper

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func pricingFixture(t *testing.T, version billing.PriceVersion) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.BillingActivePrice{}))
	previousDB := model.DB
	previousType := common.MainDatabaseType()
	previousLog := common.LogDatabaseType()
	model.DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB = previousDB
		common.SetDatabaseTypes(previousType, previousLog)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	_, err = model.PublishBillingPrice(context.Background(), version, 0, 1)
	require.NoError(t, err)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	info := &relaycommon.RelayInfo{OriginModelName: version.Model, BillingRequestInput: &billingexpr.RequestInput{Attributes: map[string]interface{}{"service_tier": "priority"}, FrozenAt: time.Date(2026, 9, 19, 3, 0, 0, 0, time.UTC)}}
	return c, info
}

func TestPublishedPricePrepaymentUsesFrozenAttributesAndDefaultOutputEstimate(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeExpression, Expression: `p * 2 + c * (attr("service_tier") == "priority" ? 4 : 1)`})
	price, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 16484, price.QuotaToPreConsume)
	require.NotNil(t, price.Quote)
	assert.Equal(t, 3, price.Quote.FrozenAt.Hour())
	assert.True(t, price.Usage.Estimated)
	previous := price.Quote
	_, err = model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "99"}, {Quantity: "c", Unit: "per_million_tokens", Price: "99"}}}, previous.PriceVersion.ID, 1)
	require.NoError(t, err)
	result, err := billing.Calculate(previous, *price.Usage)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(16484), result.FinalQuota)
}

func TestPublishedPriceRejectsMissingModelOverflowAndInvalidChannelRatio(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "p", Unit: "per_million_tokens", Price: "999999999999"}}})
	_, err := ModelPriceHelper(c, info, 100, &types.TokenCountMeta{MaxTokens: 1})
	require.Error(t, err)
	info.OriginModelName = "not-published"
	_, err = ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.Error(t, err)
	info.OriginModelName = "test"
	c.Set("channel_ratio", -1.0)
	_, err = ModelPriceHelper(c, info, 100, &types.TokenCountMeta{})
	require.Error(t, err)
}

func TestPublishedZeroEstimateStillReservesOneUnlessExplicitFree(t *testing.T) {
	c, info := pricingFixture(t, billing.PriceVersion{Model: "test", Mode: billing.ModeUnitPrice, Prices: []billing.UnitPrice{{Quantity: "calls", Unit: "per_call", Price: "0"}}})
	price, err := ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Equal(t, 1, price.QuotaToPreConsume)
	assert.False(t, price.FreeModel)
	_, err = model.PublishBillingPrice(c.Request.Context(), billing.PriceVersion{Model: "test", Mode: billing.ModeFree}, price.Quote.PriceVersion.ID, 1)
	require.NoError(t, err)
	price, err = ModelPriceHelper(c, info, 0, &types.TokenCountMeta{})
	require.NoError(t, err)
	assert.Zero(t, price.QuotaToPreConsume)
	assert.True(t, price.FreeModel)
	require.NotNil(t, price.Quote)
}
