package model

import (
	"errors"
	"fmt"
	"os"
	"strings"
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

func openNativeTopUpFacadeDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
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
	db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: fmt.Sprintf("nativepay_%d_", time.Now().UnixNano())}, Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	conn, err := db.DB()
	require.NoError(t, err)
	if dialect == "sqlite" {
		conn.SetMaxOpenConns(1)
	}
	oldDB, oldLog, oldRedis, oldUnit := DB, LOG_DB, common.RedisEnabled, common.QuotaPerUnit
	oldType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	oldPolicy, oldPayment := *operation_setting.GetAffiliateSetting(), *operation_setting.GetPaymentSetting()
	DB, LOG_DB = db, db
	common.RedisEnabled = false
	common.QuotaPerUnit = 1000
	common.SetDatabaseTypes(dbType, dbType)
	initCol()
	*operation_setting.GetAffiliateSetting() = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000}
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	tables := []any{&User{}, &TopUp{}, &Log{}, &AffiliateRecord{}, &AdminQuotaOperation{}}
	require.NoError(t, db.AutoMigrate(tables...))
	t.Cleanup(func() {
		assert.NoError(t, db.Migrator().DropTable(tables...))
		assert.NoError(t, conn.Close())
		DB, LOG_DB = oldDB, oldLog
		common.RedisEnabled = oldRedis
		common.QuotaPerUnit = oldUnit
		common.SetDatabaseTypes(oldType, oldLogType)
		initCol()
		*operation_setting.GetAffiliateSetting(), *operation_setting.GetPaymentSetting() = oldPolicy, oldPayment
	})
	return db
}

// The upstream provider facades retain the shared wallet/referral transaction.
// Exercise row locks, rollback and legacy amounts on each supported dialect.
func TestNativeTopUpFacadeDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			db := openNativeTopUpFacadeDB(t, dialect)
			for _, provider := range []string{PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake} {
				t.Run(provider, func(t *testing.T) {
					inviter := User{Username: "i-" + provider, AffCode: "i-" + provider, Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&inviter).Error)
					user := User{Username: "u-" + provider, AffCode: "u-" + provider, InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Status: common.UserStatusEnabled}
					require.NoError(t, db.Create(&user).Error)
					currency := "USD"
					if provider == PaymentProviderEpay {
						currency = "CNY"
					}
					createOrder := func(suffix string) *TopUp {
						order := &TopUp{UserId: user.Id, TradeNo: provider + suffix, PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, ExpectedQuota: 100003, Money: 1, Amount: 1, Currency: currency, PayAmount: "1"}
						require.NoError(t, order.Insert())
						return order
					}
					settle := func(order *TopUp) error {
						switch provider {
						case PaymentProviderEpay:
							_, err := RechargeEpay(order.TradeNo, "", "", "1")
							return err
						case PaymentProviderStripe:
							return Recharge(order.TradeNo, "customer", "")
						case PaymentProviderCreem:
							return RechargeCreem(order.TradeNo, "", "", "")
						case PaymentProviderWaffo:
							return RechargeWaffo(order.TradeNo, "")
						default:
							return RechargeWaffoPancake(order.TradeNo)
						}
					}
					order := createOrder("-race")
					if provider == PaymentProviderEpay {
						_, err := RechargeEpay(order.TradeNo, "", "", "0.99")
						require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
						require.NoError(t, db.First(&user, user.Id).Error)
						assert.Zero(t, user.Quota)
						assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
					}
					oldUnit := common.QuotaPerUnit
					common.QuotaPerUnit = 23
					defer func() { common.QuotaPerUnit = oldUnit }()
					results := make(chan error, 2)
					start := make(chan struct{})
					go func() { <-start; results <- settle(order) }()
					go func() { <-start; results <- ManualCompleteTopUp(order.TradeNo, "") }()
					close(start)
					for range 2 {
						err := <-results
						if err != nil {
							require.Contains(t, []string{PaymentProviderStripe, PaymentProviderCreem}, provider)
							require.EqualError(t, err, "top-up failed")
						}
					}
					retryErr := settle(order)
					if provider == PaymentProviderStripe || provider == PaymentProviderCreem {
						require.EqualError(t, retryErr, "top-up failed")
					} else {
						require.NoError(t, retryErr)
					}
					require.NoError(t, db.First(&user, user.Id).Error)
					assert.Equal(t, 100003, user.Quota)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					assert.Equal(t, 20000, inviter.AffQuota)
					saved := GetTopUpByTradeNo(order.TradeNo)
					require.NotNil(t, saved)
					assert.Equal(t, 100003, saved.CreditedQuota)
					assert.Equal(t, 100003, saved.AffiliateBaseQuota)
					if provider == PaymentProviderEpay {
						_, err := RechargeEpay(order.TradeNo, "", "", "2")
						require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
					}
					var records int64
					require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ?", inviter.Id).Count(&records).Error)
					assert.EqualValues(t, 1, records)

					rollback := createOrder("-rollback")
					callback := "native_payment_referral_failure"
					require.NoError(t, db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
						if strings.HasSuffix(tx.Statement.Table, "affiliate_records") {
							tx.AddError(errors.New("injected referral failure"))
						}
					}))
					require.Error(t, settle(rollback))
					require.NoError(t, db.Callback().Create().Remove(callback))
					require.NoError(t, db.First(&user, user.Id).Error)
					assert.Equal(t, 100003, user.Quota)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					assert.Equal(t, 20000, inviter.AffQuota)
					assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(rollback.TradeNo).Status)
					require.NoError(t, settle(rollback))
					require.NoError(t, db.First(&user, user.Id).Error)
					assert.Equal(t, 200006, user.Quota)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					assert.Equal(t, 40000, inviter.AffQuota)
				})
			}
			t.Run("stripe_legacy_money", func(t *testing.T) {
				common.QuotaPerUnit = 1000
				user := User{Username: "stripe-legacy", AffCode: "stripe-legacy", Status: common.UserStatusEnabled}
				require.NoError(t, db.Create(&user).Error)
				order := TopUp{UserId: user.Id, TradeNo: "stripe-legacy", PaymentProvider: PaymentProviderStripe, PaymentMethod: PaymentMethodStripe, Status: common.TopUpStatusPending, Amount: 999, Money: 2}
				require.NoError(t, db.Create(&order).Error)
				require.NoError(t, Recharge(order.TradeNo, "customer", ""))
				require.NoError(t, db.First(&user, user.Id).Error)
				assert.Equal(t, 2000, user.Quota)
				saved := GetTopUpByTradeNo(order.TradeNo)
				require.NotNil(t, saved)
				assert.Equal(t, int64(999), saved.Amount)
				assert.Equal(t, float64(2), saved.Money)
				assert.Equal(t, 2000, saved.CreditedQuota)
			})
		})
	}
}
