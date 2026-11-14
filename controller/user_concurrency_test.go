package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useUserPublicationRedis(t *testing.T) {
	t.Helper()
	server := miniredis.RunT(t)
	previousClient, previousEnabled := common.RDB, common.RedisEnabled
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	common.RDB, common.RedisEnabled = client, true
	// The authenticated operator must exist and have a published cache before
	// fault injection; otherwise audit logging starts an unrelated async lookup.
	operator := model.User{Id: 9999, Username: "publication-operator", AffCode: "publication-operator", Role: common.RoleRootUser, Status: common.UserStatusEnabled}
	require.NoError(t, model.DB.Create(&operator).Error)
	require.NoError(t, model.PublishUserAuthCache(operator.Id))
	t.Cleanup(func() {
		_ = client.Close()
		common.RDB, common.RedisEnabled = previousClient, previousEnabled
	})
}

func requireCommittedUserCache(t *testing.T, db *gorm.DB, committed *model.User) {
	t.Helper()
	cached, err := model.GetUserCache(committed.Id)
	require.NoError(t, err, "the normal user-cache read recovers from the committed database row")
	require.Equal(t, committed.MaxConcurrency, cached.MaxConcurrency)
	require.Equal(t, committed.AuthVersion, cached.AuthVersion)
	// A second read must use a complete, valid Redis entry rather than silently
	// passing through the database fallback when cache publication is broken.
	const callback = "test:verify-user-cache-hit"
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
			tx.AddError(errors.New("user database unavailable after cache recovery"))
		}
	}))
	defer func() { require.NoError(t, db.Callback().Query().Remove(callback)) }()
	hit, err := model.GetUserCache(committed.Id)
	require.NoError(t, err, "recovered user metadata must be readable from Redis")
	require.Equal(t, cached.MaxConcurrency, hit.MaxConcurrency)
	require.Equal(t, cached.AuthVersion, hit.AuthVersion)
}

func TestCommittedConcurrencyPersistsAcrossNativeAuthCacheFailure(t *testing.T) {
	for index, scenario := range []string{"caller cancelled after update", "auth cache publication fails", "limit publication fails without skipping auth cache"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			useUserPublicationRedis(t)
			user := model.User{Id: 801091 + index, Username: "concurrency-managed", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, service.RefreshUserConcurrencyLimit(context.Background(), user.Id))
			permit, err := service.AcquireUserConcurrency(context.Background(), user.Id, 0)
			require.NoError(t, err)
			defer permit.Release()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var published, authCacheAttempted atomic.Bool
			if index == 0 {
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:cancel-concurrency-update", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
						cancel()
					}
				}))
			} else if index == 1 {
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:observe-concurrency-publication", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" && len(tx.Statement.Selects) == 2 {
						published.Store(true)
					}
				}))
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-auth-cache-publication", func(tx *gorm.DB) {
					if published.Load() && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" && len(tx.Statement.Selects) != 2 {
						authCacheAttempted.Store(true)
						tx.AddError(errors.New("auth cache lookup unavailable"))
					}
				}))
			} else {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-limit-publication", func(tx *gorm.DB) {
					if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "User" {
						return
					}
					if len(tx.Statement.Selects) == 2 {
						published.Store(true)
						tx.AddError(errors.New("concurrency lookup unavailable"))
					} else if published.Load() {
						authCacheAttempted.Store(true)
					}
				}))
			}

			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", strings.NewReader(fmt.Sprintf(`{"id":%d,"username":%q,"max_concurrency":1}`, user.Id, user.Username))).WithContext(ctx)
			c.Set("id", 9999)
			c.Set("role", common.RoleAdminUser)
			UpdateUser(c)
			require.Equal(t, http.StatusOK, recorder.Code)
			if index == 1 {
				require.Contains(t, recorder.Body.String(), `"success":false`)
			} else {
				require.Contains(t, recorder.Body.String(), `"success":true`)
			}
			if index == 0 {
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.Contains(t, recorder.Body.String(), `"publication_pending":false`)
			} else {
				require.True(t, published.Load())
				require.True(t, authCacheAttempted.Load(), "the enabled auth-cache publication must reach the database")
				if index == 2 {
					require.Contains(t, recorder.Body.String(), `"publication_pending":true`)
				}
			}
			if index == 2 {
				require.True(t, authCacheAttempted.Load(), "a committed authentication change must still reach the auth cache")
				require.NoError(t, db.Callback().Query().Remove("test:fail-limit-publication"))
			}
			_, err = service.AcquireUserConcurrency(context.Background(), user.Id, 0)
			require.ErrorIs(t, err, service.ErrConcurrencyFull, "a stale authentication limit must not overwrite the committed limit")
			found := false
			for _, row := range service.RoutingSnapshot().Users {
				if row.ID == user.Id {
					found = true
					require.Equal(t, 1, row.Limit)
					require.Equal(t, 1, row.InFlight, "configuration publication must preserve the existing request")
				}
			}
			require.True(t, found)
			if index == 1 {
				require.NoError(t, db.Callback().Query().Remove("test:fail-auth-cache-publication"))
			}
			var committed model.User
			require.NoError(t, db.First(&committed, user.Id).Error)
			require.Equal(t, 1, committed.MaxConcurrency)
			requireCommittedUserCache(t, db, &committed)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Where("action = ?", "user.update").Find(&audits).Error)
			if index == 1 {
				require.Empty(t, audits, "native auth cache failure returns before the manage audit")
			} else {
				require.Len(t, audits, 1)
				require.True(t, audits[0].Success)
			}
		})
	}
}

func TestCommittedUpdateKeepsNativeSecurityErrorOrder(t *testing.T) {
	for _, failedAction := range []string{"concurrency", "policy", "sessions", "cache"} {
		t.Run(failedAction, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			useUserPublicationRedis(t)
			require.NoError(t, db.AutoMigrate(&model.SystemTask{}))
			require.NoError(t, authz.Init(db))
			user := model.User{Username: "committed-security", Role: common.RoleCommonUser, Status: common.UserStatusEnabled, AuthVersion: 2, MaxConcurrency: 1}
			require.NoError(t, db.Create(&user).Error)
			prior := user
			prior.AuthVersion = 1
			session := model.UserSession{SID: "committed-security-session", UserID: user.Id, Version: 1, UserAuthVersion: 1, Status: model.UserSessionStatusActive, RefreshHash: "test-refresh", ExpiresAt: time.Now().Add(time.Hour).Unix()}
			require.NoError(t, db.Create(&session).Error)
			attempted := map[string]bool{}
			var attemptedMu sync.Mutex
			require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:user-security-publication", func(tx *gorm.DB) {
				attemptedMu.Lock()
				defer attemptedMu.Unlock()
				if tx.Statement.Schema == nil {
					return
				}
				switch tx.Statement.Schema.Name {
				case "CasbinRule":
					attempted["policy"] = true
					if failedAction == "policy" {
						tx.AddError(errors.New("policy publication unavailable"))
					}
				case "UserSession":
					attempted["sessions"] = true
					if failedAction == "sessions" {
						tx.AddError(errors.New("session revocation unavailable"))
					}
				case "User":
					if len(tx.Statement.Selects) == 2 {
						attempted["concurrency"] = true
						if failedAction == "concurrency" {
							tx.AddError(errors.New("concurrency publication unavailable"))
						}
					} else if attempted["concurrency"] {
						attempted["cache"] = true
						if failedAction == "cache" {
							tx.AddError(errors.New("cache publication unavailable"))
						}
					}
				}
			}))
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest(http.MethodPut, "/api/user/", nil)
			c.Set("id", 9999)
			c.Set("role", common.RoleRootUser)
			finishCommittedUserUpdate(c, &prior, &user, true, map[string]any{"id": user.Id, "username": user.Username})
			var body struct {
				Success, Committed bool
				PublicationPending bool `json:"publication_pending"`
				Message            string
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			securityFailure := failedAction != "concurrency"
			require.Equal(t, !securityFailure, body.Success)
			require.Equal(t, !securityFailure, body.PublicationPending, "only publication with a defined recovery path is pending")
			if securityFailure {
				require.Contains(t, body.Message, "unavailable")
			}
			for _, action := range []string{"concurrency", "policy", "sessions", "cache"} {
				want := action == "concurrency" || action == "policy" || (action == "sessions" && failedAction != "policy") || (action == "cache" && failedAction != "policy" && failedAction != "sessions")
				attemptedMu.Lock()
				got := attempted[action]
				attemptedMu.Unlock()
				require.Equal(t, want, got, "native post-commit order: %s after %s failure", action, failedAction)
			}
			require.NoError(t, db.Callback().Query().Remove("test:user-security-publication"))
			requireCommittedUserCache(t, db, &user)
			var audits []model.AuditLog
			require.NoError(t, model.LOG_DB.Where("action = ?", "user.update").Find(&audits).Error)
			if securityFailure {
				require.Empty(t, audits)
			} else {
				require.Len(t, audits, 1)
				require.True(t, audits[0].Success)
			}
			require.NoError(t, db.First(&session, "sid = ?", session.SID).Error)
			if failedAction != "sessions" && failedAction != "policy" {
				require.Equal(t, model.UserSessionStatusRevoked, session.Status)
			} else {
				require.Equal(t, model.UserSessionStatusActive, session.Status)
			}
			_, _, err := service.ValidateLoginSession(service.AuthIdentity{UserID: user.Id, SessionID: session.SID, SessionVersion: 1, UserAuthVersion: 1})
			require.ErrorIs(t, err, service.ErrLoginSessionRevoked, "the committed auth version invalidates old sessions even when session cleanup fails")
			permit, err := service.AcquireUserConcurrency(context.Background(), user.Id, 0)
			require.NoError(t, err, "admission retries a failed limit publication")
			permit.Release()
		})
	}
}
