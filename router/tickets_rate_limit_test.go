package router

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// Support replies are limited per admin account, not per IP: one admin using
// up its allowance neither blocks another admin behind the same address nor
// consumes the IP bucket that user ticket writes share with login and payment.
func TestAdminTicketRepliesAreRateLimitedPerUser(t *testing.T) {
	oldDB, oldLog, oldRedis, oldSecret := model.DB, model.LOG_DB, common.RedisEnabled, common.SessionSecret
	oldType := common.MainDatabaseType()
	oldEnable, oldNum := common.CriticalRateLimitEnable, common.CriticalRateLimitNum
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "ticket-rate.db")), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserAccessToken{}, &model.AuditLog{}, &model.Log{}, &model.Option{}, &model.Ticket{}, &model.TicketMessage{}, &model.TicketAttachment{}))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	model.DB, model.LOG_DB = db, db
	common.RedisEnabled = false
	common.SessionSecret = "ticket-rate-test-secret"
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum = true, 2
	t.Setenv("TICKET_UPLOAD_DIR", t.TempDir())
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.SessionSecret = oldSecret
		common.SetMainDatabaseType(oldType)
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum = oldEnable, oldNum
		sqlDB.Close()
	})
	owner := model.User{Username: "rate-owner", AffCode: "rate-owner", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	first := model.User{Username: "rate-admin-a", AffCode: "rate-admin-a", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	second := model.User{Username: "rate-admin-b", AffCode: "rate-admin-b", Role: common.RoleRootUser, Status: common.UserStatusEnabled, AuthVersion: 1}
	sessions := map[int]string{}
	for _, u := range []*model.User{&owner, &first, &second} {
		require.NoError(t, db.Create(u).Error)
		session, err := service.CreateLoginSession(u.Id, "password", "127.0.0.1", "test")
		require.NoError(t, err)
		sessions[u.Id] = session.AccessToken
	}
	ticket := model.Ticket{UserID: owner.Id, Title: "Rate", Category: "api", Priority: "normal"}
	_, err = model.CreateTicket(&ticket, "help", owner.Id, nil)
	require.NoError(t, err)
	engine := gin.New()
	registerExtensionRoutes(engine.Group("/api"))
	reply := func(path string, userID int) int {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.WriteField("content", "reply"))
		require.NoError(t, writer.Close())
		request := httptest.NewRequest(http.MethodPost, path, &body)
		request.RemoteAddr = "198.51.100.77:4321"
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Authorization", "Bearer "+sessions[userID])
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, request)
		return response.Code
	}
	adminPath := "/api/admin/tickets/" + strconv.Itoa(ticket.ID) + "/messages"
	require.Equal(t, http.StatusOK, reply(adminPath, first.Id))
	require.Equal(t, http.StatusOK, reply(adminPath, first.Id))
	require.Equal(t, http.StatusTooManyRequests, reply(adminPath, first.Id))
	require.Equal(t, http.StatusOK, reply(adminPath, second.Id), "another admin behind the same IP keeps its own allowance")
	userPath := "/api/tickets/" + strconv.Itoa(ticket.ID) + "/messages"
	require.Equal(t, http.StatusOK, reply(userPath, owner.Id), "admin replies do not consume the IP bucket")
	require.Equal(t, http.StatusOK, reply(userPath, owner.Id))
	require.Equal(t, http.StatusTooManyRequests, reply(userPath, owner.Id), "user ticket writes stay limited per IP")
}
