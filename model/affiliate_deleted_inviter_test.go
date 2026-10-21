package model

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAffiliateDeletedInviterDoesNotBlockPaidCredit(t *testing.T) {
	for _, hardDelete := range []bool{false, true} {
		for _, enabled := range []bool{false, true} {
			for _, source := range []string{"callback", "manual", "admin_disabled", "admin_enabled"} {
				name := fmt.Sprintf("hard=%t/enabled=%t/%s", hardDelete, enabled, source)
				t.Run(name, func(t *testing.T) {
					db, inviter, invitee := affiliateTestDB(t)
					policy := operation_setting.GetAffiliateSetting()
					policy.Enabled = enabled
					policy.AdminRechargeEnabled = source == "admin_enabled"
					deleteQuery := db
					if hardDelete {
						deleteQuery = db.Unscoped()
					}
					require.NoError(t, deleteQuery.Delete(&inviter).Error)
					const quota = 500000
					var sourceType, sourceID string
					var replay func() error
					if source == "callback" || source == "manual" {
						order := testAffiliateTopUp(t, invitee.Id, "deleted-inviter-order", quota)
						sourceType, sourceID = "topup", fmt.Sprint(order.Id)
						if source == "callback" {
							_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
							require.NoError(t, err)
						} else {
							require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
						}
						replay = func() error {
							if err := ManualCompleteTopUp(order.TradeNo, ""); err != nil {
								return err
							}
							_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
							return err
						}
						got := GetTopUpByTradeNo(order.TradeNo)
						assert.Equal(t, common.TopUpStatusSuccess, got.Status)
						assert.Equal(t, quota, got.CreditedQuota)
					} else {
						result, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", quota, "deleted-inviter-operation")
						require.NoError(t, err)
						assert.False(t, result.Replayed)
						var operation AdminQuotaOperation
						require.NoError(t, db.Where("user_id = ?", invitee.Id).First(&operation).Error)
						sourceType, sourceID = "admin_quota", fmt.Sprint(operation.ID)
						replay = func() error {
							result, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", quota, "deleted-inviter-operation")
							if err == nil {
								assert.True(t, result.Replayed)
							}
							return err
						}
					}
					require.NoError(t, replay())
					require.NoError(t, db.First(&invitee, invitee.Id).Error)
					assert.Equal(t, quota, invitee.Quota)
					var records []AffiliateRecord
					require.NoError(t, db.Find(&records).Error)
					require.Len(t, records, 1)
					assert.Equal(t, inviter.Id, records[0].UserID)
					assert.Equal(t, invitee.Id, records[0].InviteeID)
					assert.Equal(t, sourceType+":"+sourceID, records[0].SourceKey)
					assert.Equal(t, quota, records[0].BaseQuota)
					assert.Equal(t, "inviter_unavailable", records[0].SkipReason)
					assert.Zero(t, records[0].RewardQuota)
					// Restoring the recipient and enabling rewards cannot change the
					// decision already committed with this paid source.
					if hardDelete {
						require.NoError(t, db.Create(&inviter).Error)
					} else {
						require.NoError(t, db.Unscoped().Model(&User{}).Where("id = ?", inviter.Id).Update("deleted_at", nil).Error)
					}
					policy.Enabled, policy.AdminRechargeEnabled = true, true
					require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
						user, err := lockAffiliateParticipantsTx(tx, invitee.Id)
						if err != nil {
							return err
						}
						return applyAffiliateRewardTx(tx, user, sourceType, sourceID, quota)
					}))
					var recorded AffiliateRecord
					require.NoError(t, db.First(&recorded, records[0].ID).Error)
					assert.Equal(t, records[0], recorded)
					require.NoError(t, db.First(&inviter, inviter.Id).Error)
					assert.Zero(t, inviter.AffQuota)
					assert.Zero(t, inviter.AffFrozenQuota)
				})
			}
		}
	}
}

func TestAffiliateDeletedInviterConcurrentCompletionCreditsOnce(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	require.NoError(t, db.Unscoped().Delete(&inviter).Error)
	order := testAffiliateTopUp(t, invitee.Id, "deleted-inviter-concurrent", 500000)
	var workers sync.WaitGroup
	results := make(chan error, 20)
	for i := 0; i < cap(results); i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			if i%2 == 0 {
				results <- ManualCompleteTopUp(order.TradeNo, "")
				return
			}
			_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
			results <- err
		}(i)
	}
	workers.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 500000, invitee.Quota)
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("skip_reason = ?", "inviter_unavailable").Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestAffiliateInviterQueryFailureStillRollsBackCredit(t *testing.T) {
	for _, source := range []string{"topup", "admin_quota"} {
		t.Run(source, func(t *testing.T) {
			db, inviter, invitee := affiliateTestDB(t)
			order := testAffiliateTopUp(t, invitee.Id, "inviter-query-failure", 500000)
			queryFailure := errors.New("simulated inviter database failure")
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("fail_inviter_lookup", func(tx *gorm.DB) {
				if user, ok := tx.Statement.Dest.(*User); ok && user.Id == inviter.Id {
					tx.AddError(queryFailure)
				}
			}))
			t.Cleanup(func() { _ = db.Callback().Query().Remove("fail_inviter_lookup") })
			if source == "topup" {
				_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
				require.ErrorIs(t, err, queryFailure)
			} else {
				_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500000, "inviter-query-failure")
				require.ErrorIs(t, err, queryFailure)
			}
			require.NoError(t, db.First(&invitee, invitee.Id).Error)
			assert.Zero(t, invitee.Quota)
			assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
			for _, table := range []any{&AffiliateRecord{}, &AdminQuotaOperation{}} {
				var count int64
				require.NoError(t, db.Model(table).Count(&count).Error)
				assert.Zero(t, count)
			}
		})
	}
}

func TestAffiliateUnavailableDecisionWriteFailureRollsBackCredit(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	require.NoError(t, db.Delete(&inviter).Error)
	order := testAffiliateTopUp(t, invitee.Id, "unavailable-decision-failure", 500000)
	writeFailure := errors.New("simulated unavailable decision storage failure")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_unavailable_decision", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_records" {
			tx.AddError(writeFailure)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("fail_unavailable_decision") })
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.ErrorIs(t, err, writeFailure)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Zero(t, invitee.Quota)
	assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
}
