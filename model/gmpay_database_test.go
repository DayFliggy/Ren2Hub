package model

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
	policy := operation_setting.GetAffiliateSetting()
	previousPolicy := *policy
	*policy = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000, AdminRechargeEnabled: true}
	payment := operation_setting.GetPaymentSetting()
	previousPayment := *payment
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() { *policy = previousPolicy; *payment = previousPayment })
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
			require.NoError(t, db.AutoMigrate(&User{}, &TopUp{}, &Log{}, &GmpayMerchantVersion{}, &GmpayOrder{}, &Ticket{}, &TicketMessage{}, &TicketAttachment{}, &AffiliateRecord{}, &AdminQuotaOperation{}))
			name := "ext-" + uuid.NewString()[:8]
			inviter := User{Username: name + "-i", AffCode: name + "-i", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&inviter).Error)
			user := User{Username: name, AffCode: name, InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&user).Error)
			order := TopUp{UserId: user.Id, TradeNo: name, PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1, Money: 7.30}
			payment := &GmpayOrder{ExpectedQuota: 500000, AmountMinor: 730, Currency: "CNY", Token: "usdt", Network: "tron"}
			merchant := &GmpayMerchantVersion{GatewayURL: "https://payments.example.test", MerchantID: name, Key: "test-only-secret"}
			require.NoError(t, CreateGmpayOrder(context.Background(), &order, payment, merchant))
			ticket := Ticket{UserID: user.Id, Title: "Payment help", Category: "billing", Priority: "normal"}
			_, err := CreateTicket(&ticket, "Please check the order", user.Id, nil)
			require.NoError(t, err)
			t.Cleanup(func() {
				assert.NoError(t, db.Where("user_id = ?", inviter.Id).Delete(&AffiliateRecord{}).Error)
				assert.NoError(t, db.Where("user_id = ?", user.Id).Delete(&AdminQuotaOperation{}).Error)
				assert.NoError(t, db.Unscoped().Where("id = ?", inviter.Id).Delete(&User{}).Error)
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
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 100000, inviter.AffQuota)
			operationID := "admin-" + name
			adminResults := make(chan error, 8)
			var adminWG sync.WaitGroup
			for i := 0; i < 8; i++ {
				adminWG.Add(1)
				go func() {
					defer adminWG.Done()
					_, err := AdjustUserQuotaWithOperation(user.Id, 99, common.RoleRootUser, "add", 500000, operationID, nil)
					adminResults <- err
				}()
			}
			adminWG.Wait()
			close(adminResults)
			for err := range adminResults {
				require.NoError(t, err)
			}
			transferID := "transfer-" + name
			transferResults := make(chan error, 8)
			for i := 0; i < 8; i++ {
				adminWG.Add(1)
				go func() {
					defer adminWG.Done()
					_, _, err := TransferAffiliateQuota(inviter.Id, transferID)
					transferResults <- err
				}()
			}
			adminWG.Wait()
			close(transferResults)
			for err := range transferResults {
				require.NoError(t, err)
			}
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 1000000, user.Quota)
			assert.Equal(t, 200000, inviter.Quota)
			assert.Zero(t, inviter.AffQuota)
			assert.Equal(t, 200000, inviter.AffHistoryQuota)
			invitees, inviteeTotal, err := ListAffiliateInvitees(inviter.Id, "desc", &common.PageInfo{Page: 1, PageSize: 20})
			require.NoError(t, err)
			require.Len(t, invitees, 1)
			assert.EqualValues(t, 1, inviteeTotal)
			assert.Equal(t, maskAffiliateName(user.Username), invitees[0].MaskedName)
			assert.Equal(t, 200000, invitees[0].RewardQuota)
			var referrals int64
			require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ?", inviter.Id).Count(&referrals).Error)
			assert.EqualValues(t, 4, referrals)
			var logs int64
			require.NoError(t, db.Model(&Log{}).Where("user_id = ? AND type = ?", user.Id, LogTypeTopup).Count(&logs).Error)
			assert.EqualValues(t, 1, logs)
			// The same reversal/repayment transaction contract must hold on each
			// configured dialect, after the tracked reward has left affiliate quota.
			correctionID := "correction-" + name
			for attempt := range 2 {
				adjusted, err := AdjustUserQuotaWithOperation(user.Id, 99, common.RoleRootUser, "subtract", 250000, correctionID, nil)
				require.NoError(t, err)
				assert.Equal(t, attempt == 1, adjusted.Replayed)
			}
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 50000, inviter.AffOffsetDebtQuota)
			_, err = AdjustUserQuotaWithOperation(user.Id, 99, common.RoleRootUser, "add", 250000, "repayment-"+name, nil)
			require.NoError(t, err)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Zero(t, inviter.AffOffsetDebtQuota)
			assert.Zero(t, inviter.AffQuota)
			policy.Enabled = false
			_, err = AdjustUserQuotaWithOperation(user.Id, 99, common.RoleRootUser, "add", 500, "skipped-"+name, nil)
			require.NoError(t, err)
			var skipped AffiliateRecord
			require.NoError(t, db.Where("user_id = ? AND skip_reason = ?", inviter.Id, "program_disabled").First(&skipped).Error)
			assert.Equal(t, 500, skipped.BaseQuota)
			assert.Equal(t, 2000, skipped.RateBps)
			assert.Zero(t, skipped.RewardQuota)
			policy.Enabled = true
			replay, err := AdjustUserQuotaWithOperation(user.Id, 99, common.RoleRootUser, "add", 500, "skipped-"+name, nil)
			require.NoError(t, err)
			assert.True(t, replay.Replayed)
			require.NoError(t, db.First(&user, user.Id).Error)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 1000500, user.Quota)
			assert.Zero(t, inviter.AffQuota)
			require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND skip_reason = ?", inviter.Id, "program_disabled").Count(&referrals).Error)
			assert.EqualValues(t, 1, referrals)
			_, updated, err := AddSupportTicketMessage(ticket.ID, user.Id, "Confirmed", nil)
			require.NoError(t, err)
			assert.Equal(t, TicketStatusReplied, updated.Status)
			require.NotNil(t, updated.AssigneeID)
			assert.Equal(t, user.Id, *updated.AssigneeID)
			_, err = UpdateTicketStatus(ticket.ID, user.Id, TicketStatusClosed)
			require.NoError(t, err)
			// Closing again is not a transition: the close time and list order stay.
			require.NoError(t, db.Model(&Ticket{}).Where("id = ?", ticket.ID).UpdateColumns(map[string]any{"closed_at": 1700000100, "updated_at": 1700000000}).Error)
			_, changed, err := UpdateAdminTicketStatus(ticket.ID, user.Id, TicketStatusClosed)
			require.NoError(t, err)
			assert.False(t, changed)
			var closed Ticket
			require.NoError(t, db.First(&closed, ticket.ID).Error)
			assert.EqualValues(t, 1700000100, closed.ClosedAt)
			assert.EqualValues(t, 1700000000, closed.UpdatedAt)
			_, _, err = AddTicketMessage(ticket.ID, user.Id, "late reply", nil)
			require.ErrorIs(t, err, ErrTicketClosed)
			_, err = UpdateTicketStatus(ticket.ID, user.Id, TicketStatusOpen)
			require.NoError(t, err)
			_, updated, err = AddTicketMessage(ticket.ID, user.Id, "thanks", nil)
			require.NoError(t, err)
			assert.Equal(t, TicketStatusOpen, updated.Status)

			// The attachment budget counts the author's own user messages in
			// the window: not support replies, not older uploads.
			since := time.Now().Add(-24 * time.Hour).Unix()
			_, _, err = AddTicketMessage(ticket.ID, user.Id, "recent", []TicketAttachment{{StorageKey: name + "-a", Size: 3 << 20}, {StorageKey: name + "-b", Size: 4 << 20}})
			require.NoError(t, err)
			old, _, err := AddTicketMessage(ticket.ID, user.Id, "old", []TicketAttachment{{StorageKey: name + "-old", Size: 50 << 20}})
			require.NoError(t, err)
			require.NoError(t, db.Model(&TicketMessage{}).Where("id = ?", old.ID).Update("created_at", since-60).Error)
			_, _, err = AddSupportTicketMessage(ticket.ID, user.Id, "support", []TicketAttachment{{StorageKey: name + "-support", Size: 60 << 20}})
			require.NoError(t, err)
			total, err := TicketAttachmentBytesSince(context.Background(), user.Id, since)
			require.NoError(t, err)
			assert.EqualValues(t, 7<<20, total)
			total, err = TicketAttachmentBytesSince(context.Background(), inviter.Id, since)
			require.NoError(t, err)
			assert.Zero(t, total)

			// A switched-asset payment arrives as the parent order's callback
			// without a chain transaction id. Concurrent duplicates credit once;
			// another local order reusing its gateway trade number before it has
			// a create response hits the receipt unique index and rolls back.
			switched := User{Username: name + "-s", AffCode: name + "-s", Role: common.RoleCommonUser, Status: common.UserStatusEnabled}
			require.NoError(t, db.Create(&switched).Error)
			parent := TopUp{UserId: switched.Id, TradeNo: name + "-parent", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1, Money: 7.30}
			other := TopUp{UserId: switched.Id, TradeNo: name + "-other", PaymentProvider: PaymentProviderGmpay, PaymentMethod: "gmpay:usdt.tron", Status: common.TopUpStatusPending, Amount: 1, Money: 7.30}
			for _, topUp := range []*TopUp{&parent, &other} {
				require.NoError(t, CreateGmpayOrder(context.Background(), topUp, &GmpayOrder{ExpectedQuota: 300000, AmountMinor: 730, Currency: "CNY", Token: "usdt", Network: "tron"}, merchant))
			}
			t.Cleanup(func() {
				for _, topUp := range []TopUp{parent, other} {
					assert.NoError(t, db.Where("top_up_id = ?", topUp.Id).Delete(&GmpayOrder{}).Error)
					assert.NoError(t, db.Where("id = ?", topUp.Id).Delete(&TopUp{}).Error)
				}
				assert.NoError(t, db.Where("user_id = ?", switched.Id).Delete(&Log{}).Error)
				assert.NoError(t, db.Unscoped().Where("id = ?", switched.Id).Delete(&User{}).Error)
			})
			parentPayment := GmpayPayment{OrderNo: parent.TradeNo, MerchantID: merchant.MerchantID, MerchantVersionID: merchant.ID, GatewayTradeNo: "parent-" + name, AmountMinor: 730, Token: "usdt", ActualAmount: "1.01", ReceiveAddress: "parent-address"}
			credited := make(chan bool, 4)
			var callbacks sync.WaitGroup
			release := make(chan struct{})
			for range 4 {
				callbacks.Add(1)
				go func() {
					defer callbacks.Done()
					<-release
					already, err := RechargeGmpay(parentPayment, "")
					assert.NoError(t, err)
					credited <- err == nil && !already
				}()
			}
			close(release)
			callbacks.Wait()
			close(credited)
			first := 0
			for ok := range credited {
				if ok {
					first++
				}
			}
			assert.Equal(t, 1, first)
			reused := parentPayment
			reused.OrderNo = other.TradeNo
			_, err = RechargeGmpay(reused, "")
			require.Error(t, err)
			require.NotErrorIs(t, err, ErrGmpayPaymentMismatch, "the receipt unique index, not a field check, refuses the reuse")
			var otherSnapshot GmpayOrder
			require.NoError(t, db.First(&otherSnapshot, other.Id).Error)
			assert.Nil(t, otherSnapshot.GatewayTradeNo)
			assert.NotEqual(t, GmpayStatusSettled, otherSnapshot.Status)
			assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(other.TradeNo).Status)
			var parentSnapshot GmpayOrder
			require.NoError(t, db.First(&parentSnapshot, parent.Id).Error)
			assert.Empty(t, parentSnapshot.SettledTransactionID)
			require.NoError(t, db.First(&switched, switched.Id).Error)
			assert.Equal(t, 300000, switched.Quota)
		})
	}
}
