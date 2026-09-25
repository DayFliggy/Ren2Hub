package controller

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestTokenAuditRecordsTargetAndFailureWithoutRawKeys(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.AuditLog{}))
	token := seedToken(t, db, 1, "audit-key-name", "audit-secret-key-never-record")
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Set("role", common.RoleCommonUser); c.Next() }, middleware.TokenOperationAudit())
	router.POST("/api/token/:id/key", GetTokenKey)
	router.PUT("/api/token/", UpdateToken)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/token/"+strconv.Itoa(token.Id)+"/key", nil))
	require.Contains(t, recorder.Body.String(), token.GetFullKey())
	recorder = httptest.NewRecorder()
	request := httptest.NewRequest("PUT", "/api/token/", strings.NewReader(`{"id":`+strconv.Itoa(token.Id)+`,"name":"attempted-change","conversion_policy":"invalid","key":"request-secret"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	require.Contains(t, recorder.Body.String(), `"success":false`)
	var entries []model.AuditLog
	require.NoError(t, db.Order("id").Find(&entries).Error)
	require.Len(t, entries, 2)
	require.Equal(t, "token.key_view", entries[0].Action)
	require.True(t, entries[0].Success)
	require.False(t, entries[1].Success)
	encoded, err := common.Marshal(entries)
	require.NoError(t, err)
	require.Contains(t, string(encoded), "audit-key-name")
	require.Contains(t, string(encoded), "attempted-change")
	require.NotContains(t, string(encoded), token.Key)
	require.NotContains(t, string(encoded), "request-secret")
}
