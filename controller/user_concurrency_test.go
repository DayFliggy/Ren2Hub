package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateUserPublishesCommittedConcurrencyBeforeOtherSideEffects(t *testing.T) {
	for index, scenario := range []string{"caller cancelled after update", "auth cache publication fails", "limit publication fails without skipping auth cache"} {
		t.Run(scenario, func(t *testing.T) {
			db := setupManageUserTestDB(t)
			user := model.User{Id: 801091 + index, Username: "concurrency-managed", Password: "password", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			require.NoError(t, service.RefreshUserConcurrencyLimit(context.Background(), user.Id))
			permit, err := service.AcquireUserConcurrency(context.Background(), user.Id, 0)
			require.NoError(t, err)
			defer permit.Release()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			published := false
			authCacheAttempted := false
			if index == 0 {
				require.NoError(t, db.Callback().Update().After("gorm:update").Register("test:cancel-concurrency-update", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" {
						cancel()
					}
				}))
			} else if index == 1 {
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:observe-concurrency-publication", func(tx *gorm.DB) {
					if tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" && len(tx.Statement.Selects) == 2 {
						published = true
					}
				}))
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-auth-cache-publication", func(tx *gorm.DB) {
					if published && tx.Statement.Schema != nil && tx.Statement.Schema.Name == "User" && len(tx.Statement.Selects) != 2 {
						tx.AddError(errors.New("auth cache lookup unavailable"))
					}
				}))
			} else {
				require.NoError(t, db.Callback().Query().Before("gorm:query").Register("test:fail-limit-publication", func(tx *gorm.DB) {
					if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "User" {
						return
					}
					if len(tx.Statement.Selects) == 2 {
						published = true
						tx.AddError(errors.New("concurrency lookup unavailable"))
					} else if published {
						authCacheAttempted = true
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
			if index == 0 {
				require.ErrorIs(t, ctx.Err(), context.Canceled)
				require.Contains(t, recorder.Body.String(), `"success":true`)
			} else {
				require.True(t, published)
				require.Contains(t, recorder.Body.String(), `"success":false`)
			}
			if index == 2 {
				require.True(t, authCacheAttempted, "a committed authentication change must still reach the auth cache")
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
		})
	}
}
