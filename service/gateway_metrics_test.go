package service

import (
	"context"
	"maps"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billing"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestGatewayMetricsShareRecoveryPredicateAndLatchStateFailure(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "metrics.db")), &gorm.Config{})
	require.NoError(t, err)
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous; sqlDB, _ := db.DB(); assert.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&model.BillingRecovery{}, &model.Task{}, &model.Midjourney{}))
	stale := time.Now().Add(-2 * BillingRecoveryStaleAfter()).Unix()
	for _, row := range []model.BillingRecovery{
		{RequestID: "active-stale", Status: model.BillingRecoveryStatusActive, UpdatedAt: stale},
		{RequestID: "active-live", Status: model.BillingRecoveryStatusActive, UpdatedAt: time.Now().Unix()},
		{RequestID: "settle-stale", Status: model.BillingRecoveryStatusSettlementPending, UpdatedAt: stale},
		{RequestID: "refund-stale", Status: model.BillingRecoveryStatusRefundPending, UpdatedAt: stale},
		{RequestID: "log-pending", Status: model.BillingRecoveryStatusSettled, ConsumptionState: model.BillingRecoveryStatePending, UpdatedAt: stale},
		{RequestID: "log-unprepared", Status: model.BillingRecoveryStatusSettled, ConsumptionState: model.BillingRecoveryStateUnprepared, UpdatedAt: stale},
		{RequestID: "usage-unconfirmed", Status: model.BillingRecoveryStatusActive, ConsumptionState: model.BillingRecoveryStateUsagePending, UpdatedAt: stale},
		{RequestID: "usage-partial", Status: model.BillingRecoveryStatusActive, ConsumptionState: model.BillingRecoveryStateUsageCollecting, UpdatedAt: stale},
		{RequestID: "completed", Status: model.BillingRecoveryStatusSettled, ConsumptionState: model.BillingRecoveryStateCompleted, UpdatedAt: stale},
		{RequestID: "refunded", Status: model.BillingRecoveryStatusRefunded, UpdatedAt: stale},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	before, err := ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "instance", before.CounterScope)
	assert.Equal(t, "database", before.RecoveryScope)
	assert.Equal(t, int64(6), before.Recovery.Pending)
	assert.Equal(t, stale, before.Recovery.OldestUpdated)
	assert.ElementsMatch(t, []string{billing.SettlementConfirmed, billing.SettlementEstimated, billing.SettlementFrozenEstimate}, slices.Collect(maps.Keys(before.Billing.Settlements)))
	rows, err := model.ListStaleBillingRecoveries(time.Now().Add(-BillingRecoveryStaleAfter()).Unix(), 100)
	require.NoError(t, err)
	requestIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		requestIDs = append(requestIDs, row.RequestID)
	}
	assert.ElementsMatch(t, []string{"active-stale", "settle-stale", "refund-stale", "log-pending", "usage-unconfirmed", "usage-partial"}, requestIDs,
		"recovery batches finish every stale request that metrics report as backlog")
	encoded, err := common.Marshal(before)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "active-stale")
	assert.NotContains(t, string(encoded), "request_id")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Set("gateway_state_output", &gatewayStateOutput{owner: model.GatewayStateBinding{UserID: 1, TokenID: 2, ChannelID: 3, CredentialID: "local-identity", EndpointID: "local-endpoint", TransportID: "local-transport"}, signatures: make(map[int]string)})
	first := BindGatewayOutput(c, []byte(`{"object":"response","id":"resp_local"}`))
	require.Error(t, first, "missing state table must prevent exposing resumable output")
	require.ErrorIs(t, BindGatewayOutput(c, []byte(`{"object":"response","id":"resp_local"}`)), first)
	after, err := ReadGatewayMetrics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, before.StateBindingFailures+1, after.StateBindingFailures)

	require.NoError(t, db.Migrator().DropTable(&model.BillingRecovery{}))
	_, err = ReadGatewayMetrics(context.Background())
	require.Error(t, err, "missing accounting state must not be reported as zero backlog")
}
