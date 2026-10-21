package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	perfmetrics "github.com/QuantumNous/new-api/pkg/perf_metrics"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPerformanceQualityValidatesHoursAndReturnsDenseSeries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, _ := newAuditTestDatabase(t, "sqlite", "")
	useLogExportDatabases(t, db, db, common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(&model.PerfMetric{}))
	call := func(query string) (int, map[string]any) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/performance/quality"+query, nil)
		GetPerformanceQuality(ctx)
		var body map[string]any
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &body))
		return recorder.Code, body
	}
	for _, query := range []string{"?hours=0", "?hours=abc", "?hours=721"} {
		status, body := call(query)
		require.Equal(t, http.StatusBadRequest, status, query)
		require.Equal(t, false, body["success"], query)
	}
	status, body := call("?hours=3")
	require.Equal(t, http.StatusOK, status)
	var result struct {
		Data perfmetrics.HourlyResult `json:"data"`
	}
	encoded, err := common.Marshal(body)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encoded, &result))
	require.Len(t, result.Data.Series, 3)
	require.EqualValues(t, 3600, result.Data.BucketSeconds)
}
