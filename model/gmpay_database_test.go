package model

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// External DSNs are opt-in and must point to disposable acceptance databases.
// Only rows created by this test are removed; the database is never dropped.
func TestGmpayAndTicketDatabaseContracts(t *testing.T) {
	for _, kind := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(kind, func(t *testing.T) {
			var db *gorm.DB
			if kind == "sqlite" {
				db = useGmpayTestDB(t)
			} else {
				key := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
				if kind == "postgres" {
					key = "REN2HUB_EXTENSION_TEST_POSTGRES_DSN"
				}
				dsn := os.Getenv(key)
				if dsn == "" {
					t.Skip(key + " is not configured; isolated database required")
				}
				var dialector gorm.Dialector = mysql.Open(dsn)
				dbType := common.DatabaseTypeMySQL
				if kind == "postgres" {
					dialector = postgres.Open(dsn)
					dbType = common.DatabaseTypePostgreSQL
				}
				var err error
				db, err = gorm.Open(dialector, &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
				require.NoError(t, err)
				previousDB, previousLog, previousRedis := DB, LOG_DB, common.RedisEnabled
				previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
				DB, LOG_DB = db, db
				common.RedisEnabled = false
				common.SetMainDatabaseType(dbType)
				common.SetLogDatabaseType(dbType)
				initCol()
				sqlDB, err := db.DB()
				require.NoError(t, err)
				t.Cleanup(func() {
					DB, LOG_DB = previousDB, previousLog
					common.RedisEnabled = previousRedis
					common.SetMainDatabaseType(previousType)
					common.SetLogDatabaseType(previousLogType)
					initCol()
					sqlDB.Close()
				})
			}
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &Log{}, &GmpayMerchantVersion{}, &GmpayOrder{}, &Ticket{}, &TicketMessage{}, &TicketAttachment{}))
			name := "ext-" + uuid.NewString()[:8]
			user := User{Username: name, AffCode: name, Group: "default"}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: name, PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1, Money: 7.30}
			payment := &GmpayOrder{ExpectedQuota: 500000, AmountMinor: 730, Currency: "CNY", Token: "usdt", Network: "tron"}
			merchant := &GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: name, Key: "test-only-secret"}
			require.NoError(t, CreateGmpayOrder(context.Background(), &order, payment, merchant))
			ticket := Ticket{UserID: user.Id, Title: "Payment help", Category: "billing", Priority: "normal"}
			_, err := CreateTicket(&ticket, "Please check the order", user.Id, nil)
			require.NoError(t, err)
			t.Cleanup(func() {
				assert.NoError(t, db.Where("ticket_id = ?", ticket.ID).Delete(&TicketAttachment{}).Error)
				assert.NoError(t, db.Where("ticket_id = ?", ticket.ID).Delete(&TicketMessage{}).Error)
				assert.NoError(t, db.Unscoped().Where("id = ?", ticket.ID).Delete(&Ticket{}).Error)
				assert.NoError(t, db.Where("top_up_id = ?", order.Id).Delete(&GmpayOrder{}).Error)
				assert.NoError(t, db.Where("id = ?", merchant.ID).Delete(&GmpayMerchantVersion{}).Error)
				assert.NoError(t, db.Where("id = ?", order.Id).Delete(&TopUp{}).Error)
				assert.NoError(t, db.Where("user_id = ?", user.Id).Delete(&Log{}).Error)
				assert.NoError(t, db.Unscoped().Where("id = ?", user.Id).Delete(&User{}).Error)
			})
			verified := GmpayPayment{OrderNo: order.TradeNo, MerchantID: merchant.MerchantID, MerchantVersionID: merchant.ID, GatewayTradeNo: "receipt-" + name, AmountMinor: 730, Token: "usdt", ActualAmount: "1.01", ReceiveAddress: "address", BlockTransactionID: "transaction-" + name}
			start := make(chan struct{})
			results := make(chan error, 2)
			var group sync.WaitGroup
			group.Add(2)
			go func() { defer group.Done(); <-start; _, err := RechargeGmpay(verified, ""); results <- err }()
			go func() { defer group.Done(); <-start; results <- ManualCompleteTopUp(order.TradeNo, "") }()
			close(start)
			group.Wait()
			close(results)
			for err := range results {
				require.NoError(t, err)
			}
			already, err := RechargeGmpay(verified, "")
			require.NoError(t, err)
			assert.True(t, already)
			require.NoError(t, db.First(&user, user.Id).Error)
			assert.Equal(t, 500000, user.Quota)
			var logs int64
			require.NoError(t, db.Model(&Log{}).Where("user_id = ? AND type = ?", user.Id, LogTypeTopup).Count(&logs).Error)
			assert.EqualValues(t, 1, logs)
			_, updated, err := AddSupportTicketMessage(ticket.ID, user.Id, "Confirmed", nil)
			require.NoError(t, err)
			assert.Equal(t, TicketStatusReplied, updated.Status)
			require.NotNil(t, updated.AssigneeID)
			assert.Equal(t, user.Id, *updated.AssigneeID)
			_, err = UpdateTicketStatus(ticket.ID, user.Id, TicketStatusClosed)
			require.NoError(t, err)
			_, _, err = AddTicketMessage(ticket.ID, user.Id, "late reply", nil)
			require.ErrorIs(t, err, ErrTicketClosed)
			_, err = UpdateTicketStatus(ticket.ID, user.Id, TicketStatusOpen)
			require.NoError(t, err)
			_, updated, err = AddTicketMessage(ticket.ID, user.Id, "thanks", nil)
			require.NoError(t, err)
			assert.Equal(t, TicketStatusOpen, updated.Status)
		})
	}
}
