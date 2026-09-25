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

func TestLogGroupSearchPreservesUserScopeAndLiteralQuery(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	connection, err := db.DB()
	require.NoError(t, err)
	connection.SetMaxOpenConns(1)
	oldDB, oldType := model.LOG_DB, common.LogDatabaseType()
	model.LOG_DB = db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.LOG_DB = oldDB
		common.SetLogDatabaseType(oldType)
		assert.NoError(t, connection.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	require.NoError(t, db.Create(&[]model.Log{
		{Id: 1, UserId: 1, Group: "a%literal"}, {Id: 2, UserId: 1, Group: "default"},
		{Id: 3, UserId: 1, Group: "default"}, {Id: 4, UserId: 2, Group: "private"},
	}).Error)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("id", 1); c.Next() })
	router.GET("/api/log/self/groups", GetLogGroups)
	router.GET("/api/log/groups", GetLogGroups)
	for _, test := range []struct{ path, expected string }{
		{"/api/log/self/groups", `["a%literal","default"]`},
		{"/api/log/self/groups?search=private&user_id=2", `[]`},
		{"/api/log/self/groups?search=%25", `["a%literal"]`},
		{"/api/log/groups", `["a%literal","default","private"]`},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
		require.Equal(t, http.StatusOK, response.Code)
		assert.JSONEq(t, `{"success":true,"data":`+test.expected+`}`, response.Body.String())
	}
}
