package controller

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestAutomaticBillingPricingConfigurationAndTaskAPI(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPricingConfig{}, &model.SystemTask{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})
	handler := billingPricingSyncHandler{}
	assert.True(t, handler.Enabled())
	assert.Equal(t, 30*time.Minute, handler.Interval())
	for _, body := range []string{`{}`, `{"enabled":null,"check_interval_minutes":30}`, `{"enabled":true,"check_interval_minutes":4}`, `{"enabled":true,"check_interval_minutes":1441}`, `{"enabled":"true","check_interval_minutes":30}`} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		SaveAutomaticBillingPricing(c)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"enabled":false,"check_interval_minutes":45}`))
	c.Request.Header.Set("Content-Type", "application/json")
	SaveAutomaticBillingPricing(c)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.False(t, handler.Enabled())
	assert.Equal(t, 45*time.Minute, handler.Interval())
	config, err := model.GetBillingPricingConfig(context.Background())
	require.NoError(t, err)
	assert.False(t, config.Enabled)
	var taskID string
	for i := 0; i < 2; i++ {
		recorder = httptest.NewRecorder()
		c, _ = gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
		SyncAutomaticBillingPricing(c)
		require.Equal(t, http.StatusOK, recorder.Code)
		var response struct {
			Success bool                     `json:"success"`
			Data    model.SystemTaskResponse `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.True(t, response.Success)
		assert.Equal(t, model.SystemTaskTypeBillingPricingSync, response.Data.Type)
		if i == 0 {
			taskID = response.Data.TaskID
		} else {
			assert.Equal(t, taskID, response.Data.TaskID, "manual sync deduplicates even with schedule disabled")
		}
	}
}

func TestRestoreAutomaticBillingPriceUsesExpectedVersionAndPreservesManualOnConflict(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.BillingPriceVersion{}, &model.BillingActivePrice{}, &model.BillingPricingSource{}, &model.Channel{}))
	previous := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previous
		connection, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	ctx := context.Background()
	document := `{"restore-model":{"input_cost_per_token":0.000002,"output_cost_per_token":0.000008}}`
	for _, source := range []string{"sub2api", "axonhub"} {
		row := model.BillingPricingSource{Source: source}
		if source == "sub2api" {
			row.Document, row.Hash = model.LongText(document), fmt.Sprintf("%x", sha256.Sum256([]byte(document)))
		}
		require.NoError(t, model.EnsureBillingPricingSource(ctx, &row))
	}
	manual, err := model.PublishBillingPrice(ctx, billing.PriceVersion{Model: "restore-model", Mode: billing.ModeFree}, 0, 1)
	require.NoError(t, err)
	for _, test := range []struct {
		body   string
		status int
	}{
		{`{"model":"restore-model"}`, http.StatusBadRequest},
		{`{"model":"restore-model","expected_version":-1}`, http.StatusBadRequest},
		{`{"model":"restore-model","expected_version":0}`, http.StatusConflict},
		{`{"model":"absent-model","expected_version":0}`, http.StatusBadRequest},
		{fmt.Sprintf(`{"model":"restore-model","expected_version":%d}`, manual.ID), http.StatusOK},
	} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
		c.Request.Header.Set("Content-Type", "application/json")
		RestoreAutomaticBillingPricing(c)
		require.Equal(t, test.status, recorder.Code, recorder.Body.String())
		current, err := model.GetActiveBillingPrice(ctx, "restore-model")
		require.NoError(t, err)
		if test.status != http.StatusOK {
			assert.Equal(t, manual.ID, current.ID)
		} else {
			assert.NotEqual(t, manual.ID, current.ID)
		}
	}
}
