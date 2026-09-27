package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingReconciliationAuditDoesNotInferRefundProfit(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingRecovery{}, &model.BillingAttempt{}, &model.BillingEvent{}, &model.BillingUpstreamCost{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.Create(&model.BillingRecovery{RequestID: "released-audit", UserID: 1, Source: "wallet", Status: model.BillingRecoveryStatusRefunded, FinalEventID: "released-audit:release"}).Error)
	require.NoError(t, db.Create(&model.BillingEvent{RequestID: "released-audit", UserID: 1, EventID: "released-audit:release", Operation: "release"}).Error)
	engine := gin.New()
	engine.GET("/audit/:id", GetBillingReconciliation)
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/audit/released-audit", nil))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Audit model.BillingAudit `json:"audit"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
	assert.True(t, payload.Success)
	assert.Equal(t, model.BillingAuditUnconfirmed, payload.Data.Audit.Status)
	assert.Equal(t, "0", payload.Data.Audit.CustomerChargeUSD)
	assert.Nil(t, payload.Data.Audit.QuotedTotalUSD)
	assert.Nil(t, payload.Data.Audit.UpstreamCostUSD)
	assert.Nil(t, payload.Data.Audit.ProfitUSD)
}
