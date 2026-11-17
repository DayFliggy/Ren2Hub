package model

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
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

// Order reversals serialize with transfers and with new top-up settlements of
// the same invitee on every dialect: one order is reversed once, balances and
// debt stay consistent with the records, and no combination deadlocks.
func TestAffiliateTopUpReversalConcurrencyDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openAffiliateLedgerDatabase(t, dialect)
			pair := func(t *testing.T, name string) (User, User) {
				inviter := User{Username: name + "-inviter", AffCode: "REV-" + name + "-A", Status: common.UserStatusEnabled}
				require.NoError(t, db.Create(&inviter).Error)
				invitee := User{Username: name + "-invitee", AffCode: "REV-" + name + "-B", InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Status: common.UserStatusEnabled}
				require.NoError(t, db.Create(&invitee).Error)
				return inviter, invitee
			}
			pending := func(t *testing.T, userID int, trade string) {
				order := &TopUp{UserId: userID, TradeNo: trade, PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, ExpectedQuota: 10000, Money: 2, Currency: "CNY", PayAmount: "2.00", CreateTime: common.GetTimestamp()}
				require.NoError(t, order.Insert())
			}
			settle := func(trade string) error {
				_, err := RechargeEpay(trade, "", "", "2.00")
				return err
			}

			t.Run("one order is reversed once", func(t *testing.T) {
				inviter, invitee := pair(t, "once")
				pending(t, invitee.Id, "rev-once")
				require.NoError(t, settle("rev-once"))
				var replays atomic.Int32
				errs := runAffiliateConcurrently(8, func(int) error {
					_, replayed, err := ReverseTopUpAffiliateReward("rev-once", common.RoleRootUser)
					if err == nil && replayed {
						replays.Add(1)
					}
					return err
				})
				for _, err := range errs {
					require.NoError(t, err)
				}
				assert.EqualValues(t, 7, replays.Load())
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota)
				assert.Zero(t, inviter.AffOffsetDebtQuota)
				assert.EqualValues(t, 1, countAffiliateKind(t, db, inviter.Id, "reversal"))
			})

			t.Run("reversal and transfer", func(t *testing.T) {
				inviter, invitee := pair(t, "transfer")
				pending(t, invitee.Id, "rev-transfer")
				require.NoError(t, settle("rev-transfer"))
				var reversal *AffiliateRecord
				errs := runAffiliateConcurrently(2, func(i int) error {
					if i == 0 {
						record, _, err := ReverseTopUpAffiliateReward("rev-transfer", common.RoleRootUser)
						reversal = record
						return err
					}
					_, _, err := TransferAffiliateQuota(inviter.Id, "rev-transfer-out")
					return err
				})
				require.NoError(t, errs[0])
				if errs[1] != nil {
					require.ErrorIs(t, errs[1], ErrAffiliateUnavailable, "only a reversal that ran first may leave nothing to transfer")
				}
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota)
				assert.Equal(t, 2000, inviter.Quota+reversal.CreditedQuota, "the reward was either transferred or revoked")
				assert.Equal(t, 2000, reversal.CreditedQuota+reversal.DebtAddedQuota)
				assert.Equal(t, reversal.DebtAddedQuota, inviter.AffOffsetDebtQuota)
			})

			t.Run("reversal and a new top-up of the same invitee", func(t *testing.T) {
				inviter, invitee := pair(t, "settle")
				for round := range 4 {
					reversed, next := fmt.Sprintf("rev-settle-%d", round), fmt.Sprintf("rev-settle-next-%d", round)
					pending(t, invitee.Id, reversed)
					require.NoError(t, settle(reversed))
					// Move the reward out so the reversal owes it as debt, which
					// the concurrent new reward repays in either order.
					_, _, err := TransferAffiliateQuota(inviter.Id, fmt.Sprintf("rev-settle-out-%d", round))
					require.NoError(t, err)
					pending(t, invitee.Id, next)
					errs := runAffiliateConcurrently(2, func(i int) error {
						if i == 0 {
							_, _, err := ReverseTopUpAffiliateReward(reversed, common.RoleRootUser)
							return err
						}
						return settle(next)
					})
					require.NoError(t, errs[0], "round %d reversal", round)
					require.NoError(t, errs[1], "round %d settlement", round)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					assert.Zero(t, inviter.AffQuota, "round %d", round)
					assert.Zero(t, inviter.AffOffsetDebtQuota, "round %d", round)
				}
				require.NoError(t, db.First(&invitee, invitee.Id).Error)
				assert.Equal(t, 80000, invitee.Quota, "the invitee's wallet keeps every top-up")
				assert.EqualValues(t, 8, countAffiliateKind(t, db, inviter.Id, "accrue"))
				assert.EqualValues(t, 4, countAffiliateKind(t, db, inviter.Id, "reversal"))
			})

			t.Run("soft-deleted inviter: reversal and administrator subtraction", func(t *testing.T) {
				inviter, invitee := pair(t, "deleted")
				const rounds = 6
				for round := range rounds {
					trade := fmt.Sprintf("rev-deleted-%d", round)
					pending(t, invitee.Id, trade)
					require.NoError(t, settle(trade))
					_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, fmt.Sprintf("rev-deleted-add-%d", round), nil)
					require.NoError(t, err)
				}
				require.NoError(t, db.First(&inviter, inviter.Id).Error)
				require.Equal(t, rounds*2200, inviter.AffQuota, "2000 per top-up plus 200 per administrator addition")
				require.NoError(t, db.Delete(&User{}, inviter.Id).Error)
				for round := range rounds {
					errs := runAffiliateConcurrently(2, func(i int) error {
						if i == 0 {
							_, _, err := ReverseTopUpAffiliateReward(fmt.Sprintf("rev-deleted-%d", round), common.RoleRootUser)
							return err
						}
						_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, fmt.Sprintf("rev-deleted-sub-%d", round), nil)
						return err
					})
					require.NoError(t, errs[0], "round %d reversal", round)
					require.NoError(t, errs[1], "round %d administrator subtraction", round)
				}
				require.NoError(t, db.Unscoped().First(&inviter, inviter.Id).Error)
				assert.Zero(t, inviter.AffQuota, "every top-up reward and every tracked reward is taken back")
				assert.Zero(t, inviter.AffOffsetDebtQuota)
				require.NoError(t, db.First(&invitee, invitee.Id).Error)
				assert.Equal(t, rounds*10000, invitee.Quota)
				assert.EqualValues(t, 2*rounds, countAffiliateKind(t, db, inviter.Id, "reversal"))
			})
		})
	}
}

func countAffiliateKind(t *testing.T, db *gorm.DB, userID int, kind string) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ?", userID, kind).Count(&count).Error)
	return count
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
