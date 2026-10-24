package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPerformanceObservationRemainsRootOnly(t *testing.T) {
	previousDB, previousLog, previousRedis, previousSecret := model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret
	previousType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.Option{}, &model.AuditLog{}))
	model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret = db, db, false, "observation-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret = previousDB, previousLog, previousRedis, previousSecret
		common.SetMainDatabaseType(previousType)
		require.NoError(t, sqlDB.Close())
	})
	engine := gin.New()
	SetApiRouter(engine)
	for _, test := range []struct {
		name         string
		role, status int
	}{{"ordinary", common.RoleCommonUser, http.StatusForbidden}, {"admin", common.RoleAdminUser, http.StatusForbidden}, {"root", common.RoleRootUser, http.StatusOK}, {"anonymous", 0, http.StatusUnauthorized}} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/performance/stats?window=6h", nil)
			if test.role > 0 {
				user := model.User{Username: test.name, AffCode: test.name, Role: test.role, Status: common.UserStatusEnabled, AuthVersion: 1}
				require.NoError(t, db.Create(&user).Error)
				session, err := service.CreateLoginSession(user.Id, "password", "127.0.0.1", "test")
				require.NoError(t, err)
				request.Header.Set("Authorization", "Bearer "+session.AccessToken)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			require.Equal(t, test.status, response.Code, response.Body.String())
			if test.role == common.RoleRootUser {
				require.Contains(t, response.Body.String(), `"observability"`)
				require.Contains(t, response.Body.String(), `"window":"6h"`)
				require.Contains(t, response.Body.String(), `"bucket_seconds":300`)
			} else {
				require.NotContains(t, response.Body.String(), `"observability"`)
				require.NotContains(t, response.Body.String(), `"db_pool"`)
			}
		})
	}
}
