package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/execution"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetRouteDiagnosticsExposesRuntimeLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/routing/diagnostics", nil)

	GetRouteDiagnostics(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool             `json:"success"`
		Data    routeDiagnostics `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	assert.Equal(t, service.DefaultRouteCandidateTopK, response.Data.Limits.CandidateTopK)
	assert.Equal(t, execution.MaxCandidates, response.Data.Limits.CandidateProbe)
	assert.Equal(t, service.DefaultTotalAttempts, response.Data.Limits.TotalSendAttempts)
	assert.Equal(t, 1024, response.Data.Limits.MaxPrecommitEvents)
	assert.Equal(t, 8<<20, response.Data.Limits.MaxPrecommitBytes)
	assert.NotNil(t, response.Data.Warnings)
	assert.NotNil(t, response.Data.RecentDecisions)
	assert.Positive(t, response.Data.GeneratedAt)
}
