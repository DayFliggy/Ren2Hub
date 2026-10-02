package model

import (
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestTokenCreationLimitsAcrossDatabases(t *testing.T) {
	t.Setenv("CRYPTO_SECRET", "token-creation-test-only-key")
	for _, backend := range []struct {
		kind common.DatabaseType
		env  string
	}{
		{common.DatabaseTypeSQLite, ""},
		{common.DatabaseTypeMySQL, "GATEWAY_STARTUP_MYSQL_DSN"},
		{common.DatabaseTypePostgreSQL, "GATEWAY_STARTUP_POSTGRES_DSN"},
	} {
		t.Run(string(backend.kind), func(t *testing.T) {
			dsn := os.Getenv(backend.env)
			if backend.env != "" && dsn == "" {
				t.Skip(backend.env + " must target an isolated database")
			}
			db, err := gatewayStartupDatabase(t, backend.kind, dsn)()
			require.NoError(t, err)
			previousDB, previousLog := DB, LOG_DB
			previousKind, previousLogKind := common.MainDatabaseType(), common.LogDatabaseType()
			previousSettings := *operation_setting.GetTokenSetting()
			DB, LOG_DB = db, db
			common.SetDatabaseTypes(backend.kind, backend.kind)
			initCol()
			connection, err := db.DB()
			require.NoError(t, err)
			if backend.kind == common.DatabaseTypeSQLite {
				connection.SetMaxOpenConns(1)
			}
			t.Cleanup(func() {
				DB, LOG_DB = previousDB, previousLog
				common.SetDatabaseTypes(previousKind, previousLogKind)
				initCol()
				*operation_setting.GetTokenSetting() = previousSettings
				assert.NoError(t, connection.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &BillingEvent{}, &UserRouteProfile{}))
			require.NoError(t, db.Create(&User{Id: 1, Username: "create-limit", Quota: 500}).Error)
			*operation_setting.GetTokenSetting() = operation_setting.TokenSetting{MaxUserTokens: 1, MaxTokenCreatesPerHour: 2}
			var tokens [2]Token
			var outcomes [2]error
			start := make(chan struct{})
			var wg sync.WaitGroup
			for i := range tokens {
				tokens[i] = Token{UserId: 1, Key: fmt.Sprintf("create-%d", i), Name: "limit", RemainQuota: 10}
				wg.Add(1)
				go func(i int) { defer wg.Done(); <-start; outcomes[i] = tokens[i].InsertWithRouteMode(RouteModeManual) }(i)
			}
			close(start)
			wg.Wait()
			require.NotEqual(t, outcomes[0] == nil, outcomes[1] == nil, "only one concurrent request may take the last slot")
			winner := 0
			for i, err := range outcomes {
				if err == nil {
					winner = i
				} else {
					assert.ErrorIs(t, err, ErrTokenCountLimit)
				}
			}
			for _, table := range []any{&Token{}, &BillingEvent{}, &UserRouteProfile{}} {
				var count int64
				require.NoError(t, db.Model(table).Count(&count).Error)
				assert.EqualValues(t, 1, count, "rejected creation leaves no partial records")
			}
			// Soft deletion permits replacing the key but retains the hourly creation.
			require.NoError(t, db.Delete(&tokens[winner]).Error)
			replacement := Token{UserId: 1, Key: "replacement", UnlimitedQuota: true}
			require.NoError(t, replacement.InsertWithRouteMode(RouteModeAutoLab))
			require.NoError(t, db.Delete(&replacement).Error)
			limited := Token{UserId: 1, Key: "limited", UnlimitedQuota: true}
			require.ErrorIs(t, limited.InsertWithRouteMode(RouteModeAutoLab), ErrTokenCreationRateLimit)
			var events, profiles, total int64
			require.NoError(t, db.Model(&BillingEvent{}).Count(&events).Error)
			require.NoError(t, db.Model(&UserRouteProfile{}).Count(&profiles).Error)
			require.NoError(t, db.Unscoped().Model(&Token{}).Count(&total).Error)
			assert.EqualValues(t, 2, events)
			assert.EqualValues(t, 2, profiles)
			assert.EqualValues(t, 2, total)
			// No sleeps or mutable clock: expire the persisted evidence explicitly.
			require.NoError(t, db.Unscoped().Model(&Token{}).Where("user_id = ?", 1).Update("created_time", common.GetTimestamp()-3601).Error)
			require.NoError(t, limited.InsertWithRouteMode(RouteModeManual))
			// Existing budget events and wallet projection remain independent of limits.
			var user User
			require.NoError(t, db.First(&user, 1).Error)
			assert.EqualValues(t, 500, user.Quota)
			assert.Zero(t, user.UsedQuota)
		})
	}
}

func TestTokenCreationRollbackDoesNotSpendRateLimit(t *testing.T) {
	db := setupBillingRecoveryTestDB(t)
	require.NoError(t, db.AutoMigrate(&UserRouteProfile{}))
	previous := *operation_setting.GetTokenSetting()
	*operation_setting.GetTokenSetting() = operation_setting.TokenSetting{MaxUserTokens: 2, MaxTokenCreatesPerHour: 1}
	t.Cleanup(func() { *operation_setting.GetTokenSetting() = previous })
	require.NoError(t, db.Create(&User{Id: 1, Username: "rollback"}).Error)
	// Force the final profile write to fail after both key and budget event writes.
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("test:reject_route_profile", func(tx *gorm.DB) {
		if tx.Statement.Table == "user_route_profiles" {
			tx.AddError(fmt.Errorf("profile write rejected"))
		}
	}))
	token := Token{UserId: 1, Key: "rolled-back", UnlimitedQuota: true}
	require.ErrorContains(t, token.InsertWithRouteMode(RouteModeManual), "profile write rejected")
	for _, table := range []any{&Token{}, &BillingEvent{}, &UserRouteProfile{}} {
		var count int64
		require.NoError(t, db.Model(table).Count(&count).Error)
		assert.Zero(t, count)
	}
	require.NoError(t, db.Callback().Create().Remove("test:reject_route_profile"))
	token = Token{UserId: 1, Key: "retry", UnlimitedQuota: true}
	require.NoError(t, token.InsertWithRouteMode(RouteModeManual))
}
