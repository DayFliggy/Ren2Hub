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

func TestAffiliateLedgerMigrationAndIdempotencyDatabases(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector = sqlite.Open(":memory:")
			dbType := common.DatabaseTypeSQLite
			if dialect != "sqlite" {
				key := "TEST_MYSQL_DSN"
				fallback := "REN2HUB_EXTENSION_TEST_MYSQL_DSN"
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
			prefix := fmt.Sprintf("affledger_%d_", time.Now().UnixNano())
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}, Logger: logger.Default.LogMode(logger.Silent)})
			require.NoError(t, err)
			conn, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				conn.SetMaxOpenConns(1)
			}
			oldDB, oldRedis, oldType := DB, common.RedisEnabled, common.MainDatabaseType()
			oldPolicy, oldPayment := *operation_setting.GetAffiliateSetting(), *operation_setting.GetPaymentSetting()
			DB, common.RedisEnabled = db, false
			common.SetMainDatabaseType(dbType)
			*operation_setting.GetAffiliateSetting() = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000, AdminRechargeEnabled: true}
			operation_setting.GetPaymentSetting().ComplianceConfirmed = true
			operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
			t.Cleanup(func() {
				DB, common.RedisEnabled = oldDB, oldRedis
				common.SetMainDatabaseType(oldType)
				*operation_setting.GetAffiliateSetting(), *operation_setting.GetPaymentSetting() = oldPolicy, oldPayment
				assert.NoError(t, db.Migrator().DropTable(&AffiliateRecord{}, &AdminQuotaOperation{}, &User{}))
				assert.NoError(t, conn.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &AffiliateRecord{}, &AdminQuotaOperation{}))
			inviter := User{Username: "legacy-inviter", AffCode: "LEDGER-INVITER", AffQuota: 50, AffHistoryQuota: 200}
			require.NoError(t, db.Create(&inviter).Error)
			invitee := User{Username: "legacy-invitee", AffCode: "LEDGER-INVITEE", InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Quota: 1000}
			require.NoError(t, db.Create(&invitee).Error)
			oldOperation := AdminQuotaOperation{OperatorID: 99, OperationID: "ledger-legacy-add", OperationKey: "99:ledger-legacy-add", UserID: invitee.Id, Value: 1000, After: 1000}
			require.NoError(t, db.Create(&oldOperation).Error)
			require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, InviteeID: invitee.Id, Kind: "accrue", SourceType: "admin_quota", SourceID: fmt.Sprint(oldOperation.ID), SourceKey: "admin_quota:" + fmt.Sprint(oldOperation.ID), BaseQuota: 1000, RateBps: 2000, RewardQuota: 200, ThawedAt: common.GetTimestamp()}).Error)
			require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, Kind: "transfer", SourceType: "transfer", SourceID: "legacy-transfer", SourceKey: "transfer:legacy", RewardQuota: 150}).Error)
			// Remove the new columns to emulate the stored schema before this
			// migration. Reapplying AutoMigrate must not infer or backfill amounts.
			for _, field := range []string{"related_record_id", "reversible", "credited_quota", "debt_offset_quota", "debt_added_quota"} {
				require.NoError(t, db.Migrator().DropColumn(&AffiliateRecord{}, field))
			}
			require.NoError(t, db.Migrator().DropColumn(&AdminQuotaOperation{}, "mode"))
			require.NoError(t, db.Migrator().DropColumn(&User{}, "aff_offset_debt_quota"))
			require.NoError(t, db.AutoMigrate(&User{}, &AffiliateRecord{}, &AdminQuotaOperation{}))
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 50, inviter.AffQuota)
			assert.Equal(t, 200, inviter.AffHistoryQuota)
			assert.Zero(t, inviter.AffOffsetDebtQuota)
			legacyReplay, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "ledger-legacy-add")
			require.NoError(t, err)
			assert.True(t, legacyReplay.Replayed)
			for attempt := range 2 {
				added, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "ledger-new-add")
				require.NoError(t, err)
				assert.Equal(t, attempt == 1, added.Replayed)
			}
			_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "override", 1000, "ledger-new-add")
			require.ErrorIs(t, err, ErrAffiliateOperationConflict)
			transfer, _, err := TransferAffiliateQuota(inviter.Id, "ledger-new-transfer")
			require.NoError(t, err)
			assert.Equal(t, 250, transfer.RewardQuota, "legacy available 50 plus new traced reward 200")
			for index := range 2 {
				_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 500, fmt.Sprintf("ledger-reverse-%d", index))
				require.NoError(t, err)
			}
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 200, inviter.AffOffsetDebtQuota)
			_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "override", 2000, "ledger-repay-override")
			require.NoError(t, err)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Zero(t, inviter.AffOffsetDebtQuota)
			assert.Zero(t, inviter.AffQuota)
			type outcome struct {
				adjustment *UserQuotaAdjustment
				err        error
			}
			concurrentSubtract := func(operationIDs []string) int {
				start := make(chan struct{})
				results := make(chan outcome, len(operationIDs))
				for _, operationID := range operationIDs {
					go func(id string) {
						<-start
						adjustment, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 250, id)
						results <- outcome{adjustment, err}
					}(operationID)
				}
				close(start)
				replays := 0
				for range operationIDs {
					result := <-results
					require.NoError(t, result.err)
					if result.adjustment.Replayed {
						replays++
					}
				}
				return replays
			}
			assert.Equal(t, 1, concurrentSubtract([]string{"ledger-same-subtract", "ledger-same-subtract"}))
			assert.Zero(t, concurrentSubtract([]string{"ledger-distinct-one", "ledger-distinct-two"}))
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			require.NoError(t, db.First(&invitee, invitee.Id).Error)
			assert.Equal(t, 1250, invitee.Quota, "one replay and two distinct reductions commit exactly three times")
			assert.Equal(t, 150, inviter.AffOffsetDebtQuota, "each committed reduction restores only its share of repaid debt")
			info, err := GetAffiliateInfo(inviter.Id, time.UTC)
			require.NoError(t, err)
			assert.True(t, info.TransferEnabled, "compliance capability is independent of zero available quota")
			operation_setting.GetPaymentSetting().ComplianceConfirmed = false
			info, err = GetAffiliateInfo(inviter.Id, time.UTC)
			require.NoError(t, err)
			assert.False(t, info.TransferEnabled)
			records, total, err := ListAffiliateRecords(inviter.Id, "", &common.PageInfo{Page: 1, PageSize: 100})
			require.NoError(t, err)
			assert.EqualValues(t, len(records), total)
			for _, record := range records {
				assert.NotEqual(t, "transfer_allocation", record.Kind)
			}
			// A committed transfer is only a receipt lookup. It must remain
			// replayable after compliance is revoked, including on dialects
			// that use FOR UPDATE to serialize simultaneous requests.
			operation_setting.GetPaymentSetting().ComplianceConfirmed = false
			beforeQuota := invitee.Quota
			replay := func() (*AffiliateRecord, bool, error) {
				return TransferAffiliateQuota(inviter.Id, "ledger-new-transfer")
			}
			if dialect == "sqlite" {
				for range 6 {
					transfer, replayed, err := replay()
					assert.NoError(t, err)
					assert.True(t, replayed)
					assert.Equal(t, 250, transfer.RewardQuota)
				}
			} else {
				type replayResult struct {
					record   *AffiliateRecord
					replayed bool
					err      error
				}
				start := make(chan struct{})
				results := make(chan replayResult, 6)
				for range 6 {
					go func() {
						<-start
						record, replayed, err := replay()
						results <- replayResult{record, replayed, err}
					}()
				}
				close(start)
				for range 6 {
					result := <-results
					assert.NoError(t, result.err)
					assert.True(t, result.replayed)
					assert.Equal(t, 250, result.record.RewardQuota)
				}
			}
			var transfers int64
			require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ? AND source_id = ?", inviter.Id, "transfer", "ledger-new-transfer").Count(&transfers).Error)
			assert.EqualValues(t, 1, transfers)
			require.NoError(t, db.First(&invitee, invitee.Id).Error)
			assert.Equal(t, beforeQuota, invitee.Quota, "transfer replays must not credit the wallet twice")
		})
	}
}
