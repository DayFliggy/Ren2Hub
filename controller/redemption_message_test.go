package controller

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// A legacy zero-quota row can still appear in the console's status-only flow.
// Its model error must reach the console with a translation key and no write.
func TestRedemptionStatusOnlyPreservesZeroQuotaErrorMessage(t *testing.T) {
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousMain, previousLog := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousMode := common.RedisEnabled, gin.Mode()
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousMain, previousLog)
		common.RedisEnabled = previousRedis
		gin.SetMode(previousMode)
	})
	db := openTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Redemption{}))
	code := model.Redemption{
		Name: "legacy-zero-quota", Key: "00000000000000000000000000000001",
		Quota: 1, Status: common.RedemptionCodeStatusEnabled,
	}
	require.NoError(t, db.Create(&code).Error)
	// Seed the legacy value explicitly; Create applies the model's quota default.
	require.NoError(t, db.Model(&code).Update("quota", 0).Error)
	require.NoError(t, db.First(&code, code.Id).Error)
	require.Zero(t, code.Quota)

	router := gin.New()
	router.PUT("/api/redemption/", UpdateRedemption)
	request := httptest.NewRequest(http.MethodPut, "/api/redemption/?status_only=true",
		bytes.NewBufferString(fmt.Sprintf(`{"id":%d,"status":%d}`, code.Id, common.RedemptionCodeStatusDisabled)))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `{"success":false,"message":"redemption quota must be positive","message_key":"redemption quota must be positive"}`, response.Body.String())
	var stored model.Redemption
	require.NoError(t, db.First(&stored, code.Id).Error)
	require.Equal(t, code, stored, "rejected status-only update must preserve the legacy row")
}
