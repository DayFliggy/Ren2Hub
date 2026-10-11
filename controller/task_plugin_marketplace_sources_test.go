package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// An empty marketplace source list persists as "[]", which reads back as a
// deliberate empty market instead of the builtin defaults. The endpoint must
// reject it so a client cannot leave the market permanently unusable.
func TestUpdateTaskPluginMarketplaceSourcesRejectsEmptyList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previousDB })
	require.NoError(t, db.AutoMigrate(&model.Option{}))

	previousMap := common.OptionMap
	common.OptionMap = map[string]string{}
	t.Cleanup(func() { common.OptionMap = previousMap })

	router := gin.New()
	router.PUT("/api/plugin/task/marketplace/sources", UpdateTaskPluginMarketplaceSources)

	put := func(body string) map[string]any {
		t.Helper()
		request := httptest.NewRequest(http.MethodPut, "/api/plugin/task/marketplace/sources", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		response := map[string]any{}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response
	}

	valid := `[{"name":"Official","index_url":"https://example.com/index.json"}]`
	require.Equal(t, true, put(valid)["success"])
	require.Len(t, setting.GetTaskPluginMarketplaceSources(), 1)

	for _, body := range []string{`[]`, `null`} {
		response := put(body)
		assert.Equal(t, false, response["success"], "body %s must be rejected", body)
		assert.Equal(t, "At least one marketplace source is required", response["message_key"])
		stored := setting.GetTaskPluginMarketplaceSources()
		require.Len(t, stored, 1, "rejected body %s must not change the stored sources", body)
		assert.Equal(t, "Official", stored[0].Name)
	}
}
