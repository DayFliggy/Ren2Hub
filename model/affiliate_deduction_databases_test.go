package model

import (
	"fmt"
	"os"
	"sync"
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

// Concurrent administrator deductions and referral transfers serialize on the
// user lock on every dialect: the available referral quota never goes below
// zero, one operation ID deducts once, and a deduction never takes the part
// still attributed to a reversible administrator-addition reward.
func TestAffiliateDeductionConcurrencyDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openAffiliateLedgerDatabase(t, dialect)

			t.Run("parallel deductions stop at the available quota", func(t *testing.T) {
				user := User{Username: "deduct-db-parallel", AffCode: "DEDUCT-DB-PARALLEL", AffQuota: 1000, AffHistoryQuota: 1000}
				require.NoError(t, db.Create(&user).Error)
				errs := runAffiliateConcurrently(8, func(i int) error {
					_, _, err := DeductAffiliateQuota(user.Id, common.RoleRootUser, 300, fmt.Sprintf("deduct-db-parallel-%d", i))
					return err
				})
				succeeded := 0
				for _, err := range errs {
					if err == nil {
						succeeded++
						continue
					}
					assert.ErrorIs(t, err, ErrAffiliateDeductionExceeds)
				}
				assert.Equal(t, 3, succeeded)
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, 100, user.AffQuota)
				assert.Equal(t, 1000, user.AffHistoryQuota)
				assert.EqualValues(t, 3, countAffiliateDeductions(t, db, user.Id))
			})

			t.Run("one operation id deducts once", func(t *testing.T) {
				user := User{Username: "deduct-db-replay", AffCode: "DEDUCT-DB-REPLAY", AffQuota: 1000}
				require.NoError(t, db.Create(&user).Error)
				var mu sync.Mutex
				ids := map[int]bool{}
				replays := 0
				errs := runAffiliateConcurrently(8, func(int) error {
					record, replayed, err := DeductAffiliateQuota(user.Id, common.RoleRootUser, 250, "deduct-db-same-operation")
					if err != nil {
						return err
					}
					mu.Lock()
					defer mu.Unlock()
					ids[record.ID] = true
					if replayed {
						replays++
					}
					return nil
				})
				for _, err := range errs {
					require.NoError(t, err)
				}
				assert.Len(t, ids, 1)
				assert.Equal(t, 7, replays)
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, 750, user.AffQuota)
				assert.EqualValues(t, 1, countAffiliateDeductions(t, db, user.Id))
			})

			t.Run("deduction and transfer keep the tracked reward", func(t *testing.T) {
				inviter := User{Username: "deduct-db-inviter", AffCode: "DEDUCT-DB-INVITER", AffQuota: 500, AffHistoryQuota: 500}
				require.NoError(t, db.Create(&inviter).Error)
				invitee := User{Username: "deduct-db-invitee", AffCode: "DEDUCT-DB-INVITEE", InviterId: inviter.Id, InvitedAt: common.GetTimestamp()}
				require.NoError(t, db.Create(&invitee).Error)
				_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "deduct-db-admin-add", nil)
				require.NoError(t, err)
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				require.Equal(t, 700, inviter.AffQuota, "500 fungible plus 200 tracked")

				var deducted *AffiliateRecord
				errs := runAffiliateConcurrently(2, func(i int) error {
					if i == 0 {
						record, _, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 500, "deduct-db-race-deduct")
						deducted = record
						return err
					}
					_, _, err := TransferAffiliateQuota(inviter.Id, "deduct-db-race-transfer")
					return err
				})
				require.NoError(t, errs[1])
				deductedQuota := 0
				if errs[0] == nil {
					deductedQuota = deducted.RewardQuota
				} else {
					require.ErrorIs(t, errs[0], ErrAffiliateDeductionExceeds, "only a transfer that ran first may leave nothing to deduct")
				}
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota)
				assert.Equal(t, 700, inviter.Quota+deductedQuota)
				var allocated int64
				require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ? AND related_record_id > 0", inviter.Id, "transfer_allocation").Select("COALESCE(SUM(reward_quota), 0)").Scan(&allocated).Error)
				assert.EqualValues(t, 200, allocated, "the tracked reward is always transferred, never deducted")
			})
		})
	}
}

func runAffiliateConcurrently(n int, run func(int) error) []error {
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = run(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

func countAffiliateDeductions(t *testing.T, db *gorm.DB, userID int) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ?", userID, "deduction").Count(&count).Error)
	return count
}

// openAffiliateLedgerDatabase opens an isolated database of the dialect with
// the tables the referral ledger, top-up settlement and its logs use.
func openAffiliateLedgerDatabase(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	var driver gorm.Dialector = sqlite.Open(":memory:")
	dbType := common.DatabaseTypeSQLite
	if dialect != "sqlite" {
		key, fallback := "TEST_MYSQL_DSN", "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
		if dialect == "postgres" {
			key, fallback = "TEST_POSTGRES_DSN", "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
		}
		dsn := os.Getenv(key)
		if dsn == "" {
			dsn = os.Getenv(fallback)
		}
		if dsn == "" {
			t.Skip(key + " is not configured; isolated database required")
		}
		driver, dbType = mysql.Open(dsn), common.DatabaseTypeMySQL
		if dialect == "postgres" {
			driver, dbType = postgres.Open(dsn), common.DatabaseTypePostgreSQL
		}
	}
	prefix := fmt.Sprintf("affdb_%d_", time.Now().UnixNano())
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		conn.SetMaxOpenConns(1)
	}
	tables := []any{&User{}, &TopUp{}, &Log{}, &AffiliateRecord{}, &AdminQuotaOperation{}}
	oldDB, oldLog, oldRedis := DB, LOG_DB, common.RedisEnabled
	oldType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldPolicy, oldPayment := *operation_setting.GetAffiliateSetting(), *operation_setting.GetPaymentSetting()
	DB, LOG_DB, common.RedisEnabled = db, db, false
	common.SetDatabaseTypes(dbType, dbType)
	initCol()
	*operation_setting.GetAffiliateSetting() = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000, AdminRechargeEnabled: true}
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		DB, LOG_DB, common.RedisEnabled = oldDB, oldLog, oldRedis
		common.SetDatabaseTypes(oldType, oldLogType)
		initCol()
		*operation_setting.GetAffiliateSetting(), *operation_setting.GetPaymentSetting() = oldPolicy, oldPayment
		assert.NoError(t, db.Migrator().DropTable(tables...))
		assert.NoError(t, conn.Close())
	})
	require.NoError(t, db.AutoMigrate(tables...))
	return db
}
