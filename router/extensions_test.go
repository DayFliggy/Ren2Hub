package router

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestExtensionsSessionOwnershipAndPATScopes(t *testing.T) {
	oldDB, oldLog, oldRedis, oldSecret := model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret
	oldType := common.MainDatabaseType()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "extension-auth.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Option{}, &model.Ticket{}, &model.TicketMessage{}, &model.TicketAttachment{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SessionSecret = "extension-auth-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.SessionSecret = oldSecret
		common.SetMainDatabaseType(oldType)
		sqlDB.Close()
	})
	owner := model.User{Username: "owner", AffCode: "owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, Group: "default"}
	other := model.User{Username: "other", AffCode: "other", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1, Group: "default"}
	root := model.User{Username: "root", AffCode: "root", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1, Group: "default"}
	for _, u := range []*model.User{&owner, &other, &root} {
		require.NoError(t, db.Create(u).Error)
	}
	ownerSession, err := service.CreateLoginSession(owner.Id, "password", "127.0.0.1", "test")
	require.NoError(t, err)
	otherSession, err := service.CreateLoginSession(other.Id, "password", "127.0.0.1", "test")
	require.NoError(t, err)
	raw := "nap_extension-private-test-token"
	pat := model.UserAccessToken{UserId: root.Id, Name: "read tickets", TokenHash: model.AccessTokenFingerprint(raw), ExpiresAt: time.Now().Add(time.Hour).Unix(), CreatedAt: time.Now().Unix()}
	require.NoError(t, pat.SetScopes([]string{"ticket:read", "wallet:write"}))
	require.NoError(t, db.Create(&pat).Error)
	ticket := model.Ticket{UserID: owner.Id, Title: "Private support", Category: "billing", Priority: "normal"}
	_, err = model.CreateTicket(&ticket, "Private message", owner.Id, nil)
	require.NoError(t, err)
	engine := gin.New()
	registerExtensionRoutes(engine.Group("/api"))
	for _, tc := range []struct {
		name, method, path, token, body, contains string
		status                                    int
	}{
		{"owner session", http.MethodGet, "/api/tickets/1", ownerSession.AccessToken, "", "Private message", http.StatusOK},
		{"other session", http.MethodGet, "/api/tickets/1", otherSession.AccessToken, "", "NOT_FOUND", http.StatusOK},
		{"personal PAT denied", http.MethodGet, "/api/tickets/1", raw, "", "AUTH_SESSION_REQUIRED", http.StatusForbidden},
		{"GMPay PAT denied", http.MethodPost, "/api/user/gmpay/pay", raw, `{"amount":1}`, "AUTH_SESSION_REQUIRED", http.StatusForbidden},
		{"admin read grant", http.MethodGet, "/api/admin/tickets/1", raw, "", "Private message", http.StatusOK},
		{"admin missing manage grant", http.MethodPatch, "/api/admin/tickets/1/status", raw, `{"status":"closed"}`, "ACCESS_TOKEN_SCOPE_DENIED", http.StatusForbidden},
		{"anonymous", http.MethodGet, "/api/tickets/1", "", "", "Unauthorized", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			request.Header.Set("Content-Type", "application/json")
			if tc.token != "" {
				request.Header.Set("Authorization", "Bearer "+tc.token)
			}
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			assert.Equal(t, tc.status, response.Code, response.Body.String())
			assert.Contains(t, response.Body.String(), tc.contains)
		})
	}
	for _, route := range engine.Routes() {
		if strings.HasSuffix(route.Path, "/notify") {
			continue
		}
		rule, ok := middleware.AccessTokenRouteRule(route.Method + " " + route.Path)
		require.True(t, ok)
		if strings.HasPrefix(route.Path, "/api/admin/tickets") {
			assert.Equal(t, "scope", rule.Kind())
			assert.True(t, strings.HasPrefix(rule.Scope(), "ticket:"))
		} else {
			assert.Equal(t, "session", rule.Kind())
		}
	}
}
