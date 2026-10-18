package model

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/schema"
)

func TestAffiliateDeletedInviterDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			dbType := common.DatabaseTypeSQLite
			if dialect != "sqlite" {
				key := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
				if dialect == "postgres" {
					key = "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
				}
				dsn := os.Getenv(key)
				if dsn == "" {
					t.Skip(key + " is not configured; isolated database required")
				}
				driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
				if dialect == "postgres" {
					driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
				}
			}
			prefix := fmt.Sprintf("affdel_%d_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				conn.SetMaxOpenConns(1)
			}
			previousDB, previousRedis, previousType := DB, common.RedisEnabled, common.MainDatabaseType()
			previousPolicy := *operation_setting.GetAffiliateSetting()
			DB, common.RedisEnabled = db, false
			common.SetMainDatabaseType(dbType)
			t.Cleanup(func() {
				DB, common.RedisEnabled = previousDB, previousRedis
				common.SetMainDatabaseType(previousType)
				*operation_setting.GetAffiliateSetting() = previousPolicy
				require.NoError(t, db.Migrator().DropTable(&AffiliateRecord{}, &AdminQuotaOperation{}, &TopUp{}, &User{}))
				require.NoError(t, conn.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &AffiliateRecord{}, &AdminQuotaOperation{}))
			for _, hardDelete := range []bool{false, true} {
				for _, enabled := range []bool{false, true} {
					t.Run(fmt.Sprintf("hard=%t/enabled=%t", hardDelete, enabled), func(t *testing.T) {
						*operation_setting.GetAffiliateSetting() = operation_setting.AffiliateSetting{Enabled: enabled, AdminRechargeEnabled: enabled, RateBps: 2000}
						name := fmt.Sprintf("user-%t-%t", hardDelete, enabled)
						inviter := User{Username: name + "-i", AffCode: name + "-i", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
						require.NoError(t, db.Create(&inviter).Error)
						user := User{Username: name, AffCode: name, InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
						require.NoError(t, db.Create(&user).Error)
						query := db
						if hardDelete {
							query = db.Unscoped()
						}
						require.NoError(t, query.Delete(&inviter).Error)
						order := testAffiliateTopUp(t, user.Id, name, 500000)
						for range 2 {
							require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
								var locked TopUp
								if err := lockForUpdate(tx).First(&locked, order.Id).Error; err != nil {
									return err
								}
								_, err := completeTopUpTx(tx, &locked, 0, nil)
								return err
							}))
						}
						for attempt := range 2 {
							result, err := AdjustUserQuotaWithOperation(user.Id, 99, common.RoleRootUser, "add", 250000, "admin-"+name)
							require.NoError(t, err)
							assert.Equal(t, attempt == 1, result.Replayed)
						}
						require.NoError(t, db.First(&user, user.Id).Error)
						assert.Equal(t, 750000, user.Quota)
						var records []AffiliateRecord
						require.NoError(t, db.Where("invitee_id = ?", user.Id).Find(&records).Error)
						require.Len(t, records, 2)
						for _, record := range records {
							assert.Equal(t, inviter.Id, record.UserID)
							assert.Equal(t, "inviter_unavailable", record.SkipReason)
							assert.Zero(t, record.RewardQuota)
						}
						assert.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo(order.TradeNo).Status)
					})
				}
			}
		})
	}
}
