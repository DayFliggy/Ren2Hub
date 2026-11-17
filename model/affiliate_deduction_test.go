package model

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func affiliateDeductionRecords(t *testing.T, db *gorm.DB, userID int) []AffiliateRecord {
	t.Helper()
	var rows []AffiliateRecord
	require.NoError(t, db.Where("user_id = ? AND kind = ?", userID, "deduction").Order("id ASC").Find(&rows).Error)
	return rows
}

// A deduction lowers only the available referral quota. The wallet, the
// lifetime reward, the frozen quota, the offset debt and the accrual rows stay
// as they were, and the record carries the balances after the deduction.
func TestDeductAffiliateQuotaReducesOnlyAvailableReward(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"quota": 7777, "aff_quota": 1000, "aff_history": 1500, "aff_frozen_quota": 300, "aff_offset_debt_quota": 50}).Error)
	require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, Kind: "accrue", SourceType: "topup", SourceID: "1", SourceKey: "topup:seeded", BaseQuota: 5000, RateBps: 2000, RewardQuota: 1000, CreditedQuota: 1000, ThawedAt: 1}).Error)
	// The program switch and payment compliance do not gate a correction.
	operation_setting.GetAffiliateSetting().Enabled = false
	operation_setting.GetPaymentSetting().ComplianceConfirmed = false

	record, replayed, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 400, "deduct-reduce-one")
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, "deduction", record.Kind)
	assert.Equal(t, "admin_deduction", record.SourceType)
	assert.Equal(t, "deduct-reduce-one", record.SourceID)
	assert.Equal(t, 400, record.RewardQuota)
	assert.Equal(t, 600, record.AvailableAfter)
	assert.Equal(t, 300, record.FrozenAfter)
	assert.Zero(t, record.InviteeID)

	var after User
	require.NoError(t, db.First(&after, inviter.Id).Error)
	assert.Equal(t, 600, after.AffQuota)
	assert.Equal(t, 7777, after.Quota)
	assert.Equal(t, 1500, after.AffHistoryQuota)
	assert.Equal(t, 300, after.AffFrozenQuota)
	assert.Equal(t, 50, after.AffOffsetDebtQuota)
	var accrual AffiliateRecord
	require.NoError(t, db.Where("source_key = ?", "topup:seeded").First(&accrual).Error)
	assert.Equal(t, 1000, accrual.CreditedQuota)
	var allocations int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("kind = ?", "transfer_allocation").Count(&allocations).Error)
	assert.Zero(t, allocations)

	rows, total, err := ListAffiliateRecords(inviter.Id, "deduction", &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, rows, 1)
	assert.Equal(t, record.ID, rows[0].ID)
}

// Invalid requests and amounts beyond the deductible quota are refused before
// anything is written.
func TestDeductAffiliateQuotaRejectsInvalidRequestsWithoutWriting(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": 600, "aff_history": 600}).Error)
	for _, tc := range []struct {
		name      string
		userID    int
		quota     int
		operation string
		want      error
	}{
		{"operation id too short", inviter.Id, 100, "short", ErrAffiliateOperation},
		{"operation id with a space", inviter.Id, 100, "deduct with space", ErrAffiliateOperation},
		{"zero amount", inviter.Id, 0, "deduct-invalid-zero", ErrAffiliateDeductionInvalid},
		{"negative amount", inviter.Id, -1, "deduct-invalid-negative", ErrAffiliateDeductionInvalid},
		{"above the wallet limit", inviter.Id, common.MaxWalletQuota + 1, "deduct-invalid-limit", ErrAffiliateDeductionInvalid},
		{"no user id", 0, 100, "deduct-invalid-user", ErrAffiliateDeductionInvalid},
		{"missing user", inviter.Id + 1000, 100, "deduct-missing-user", gorm.ErrRecordNotFound},
		{"above the deductible quota", inviter.Id, 601, "deduct-too-much", ErrAffiliateDeductionExceeds},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, replayed, err := DeductAffiliateQuota(tc.userID, common.RoleRootUser, tc.quota, tc.operation)
			require.ErrorIs(t, err, tc.want)
			assert.Nil(t, record)
			assert.False(t, replayed)
		})
	}
	var after User
	require.NoError(t, db.First(&after, inviter.Id).Error)
	assert.Equal(t, 600, after.AffQuota)
	var records int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Count(&records).Error)
	assert.Zero(t, records)
}

// The same operation replays its first result without deducting again; the
// same operation with another amount, or a recorded row of another kind under
// its key, is a conflict. Operation ids are scoped to the target user.
func TestDeductAffiliateQuotaReplaysAndRejectsConflicts(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id IN ?", []int{inviter.Id, invitee.Id}).Updates(map[string]any{"aff_quota": 1000, "aff_history": 1000}).Error)

	first, replayed, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 100, "deduct-replay-one")
	require.NoError(t, err)
	require.False(t, replayed)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Update("aff_quota", 950).Error)
	again, replayed, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 100, "deduct-replay-one")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, 900, again.AvailableAfter, "a replay returns the first deduction's balances")
	var after User
	require.NoError(t, db.First(&after, inviter.Id).Error)
	assert.Equal(t, 950, after.AffQuota, "a replay does not deduct again")

	_, _, err = DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 101, "deduct-replay-one")
	require.ErrorIs(t, err, ErrAffiliateOperationConflict)

	require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, Kind: "transfer", SourceType: "transfer", SourceID: "deduct-forged-one", SourceKey: fmt.Sprintf("admin_deduction:%d:deduct-forged-one", inviter.Id), RewardQuota: 100}).Error)
	_, _, err = DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 100, "deduct-forged-one")
	require.ErrorIs(t, err, ErrAffiliateOperationConflict)

	other, replayed, err := DeductAffiliateQuota(invitee.Id, common.RoleRootUser, 100, "deduct-replay-one")
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, invitee.Id, other.UserID)
	assert.Len(t, affiliateDeductionRecords(t, db, inviter.Id), 1)
	assert.Len(t, affiliateDeductionRecords(t, db, invitee.Id), 1)
}

// Due frozen rewards are thawed before the limit is checked; rewards that are
// still frozen cannot be deducted.
func TestDeductAffiliateQuotaThawsDueRewardsAndKeepsFrozenOnes(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	now := common.GetTimestamp()
	require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, InviteeID: invitee.Id, Kind: "accrue", SourceType: "topup", SourceID: "due", SourceKey: "topup:due", BaseQuota: 1500, RateBps: 2000, RewardQuota: 300, CreditedQuota: 300, FrozenUntil: now - 10}).Error)
	require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, InviteeID: invitee.Id, Kind: "accrue", SourceType: "topup", SourceID: "later", SourceKey: "topup:later", BaseQuota: 1000, RateBps: 2000, RewardQuota: 200, CreditedQuota: 200, FrozenUntil: now + 3600}).Error)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_frozen_quota": 500, "aff_history": 500}).Error)

	record, _, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 300, "deduct-thawed-one")
	require.NoError(t, err)
	assert.Zero(t, record.AvailableAfter)
	assert.Equal(t, 200, record.FrozenAfter)
	var due AffiliateRecord
	require.NoError(t, db.Where("source_key = ?", "topup:due").First(&due).Error)
	assert.Positive(t, due.ThawedAt)

	_, _, err = DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 1, "deduct-frozen-one")
	require.ErrorIs(t, err, ErrAffiliateDeductionExceeds)
	var after User
	require.NoError(t, db.First(&after, inviter.Id).Error)
	assert.Zero(t, after.AffQuota)
	assert.Equal(t, 200, after.AffFrozenQuota)
}

// Reward still attributed to an administrator addition is outside the
// deductible quota. After a deduction, reversing that addition and
// transferring the rest both succeed with the balances, debt and allocation
// rows they would have had without the deduction: nothing is charged twice.
func TestDeductAffiliateQuotaLeavesTrackedRewardForReversalAndTransfer(t *testing.T) {
	setup := func(t *testing.T) (*gorm.DB, User, User, AffiliateRecord) {
		db, inviter, invitee := affiliateTestDB(t)
		operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
		// 1000 added by an administrator: 200 tracked reward.
		_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "deduct-tracked-add", nil)
		require.NoError(t, err)
		// A 1500 top-up: 300 fungible reward.
		order := testAffiliateTopUp(t, invitee.Id, "deduct-tracked-topup", 1500)
		_, err = RechargeEpay(order.TradeNo, "", "", "2.00")
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		require.Equal(t, 500, inviter.AffQuota)
		var tracked AffiliateRecord
		require.NoError(t, db.Where("user_id = ? AND kind = ? AND reversible = ?", inviter.Id, "accrue", true).First(&tracked).Error)

		_, _, err = DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 301, "deduct-tracked-over")
		require.ErrorIs(t, err, ErrAffiliateDeductionExceeds, "the tracked 200 is not deductible")
		record, _, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 300, "deduct-tracked-all")
		require.NoError(t, err)
		require.Equal(t, 200, record.AvailableAfter)
		return db, inviter, invitee, tracked
	}

	t.Run("reversal", func(t *testing.T) {
		db, inviter, invitee, tracked := setup(t)
		_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, "deduct-tracked-subtract", nil)
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Zero(t, inviter.AffOffsetDebtQuota, "the reversal takes the tracked 200 from the balance, not as debt")
		var reversal AffiliateRecord
		require.NoError(t, db.Where("kind = ? AND related_record_id = ?", "reversal", tracked.ID).First(&reversal).Error)
		assert.Equal(t, 200, reversal.RewardQuota)
		assert.Equal(t, 200, reversal.CreditedQuota)
		assert.Zero(t, reversal.DebtAddedQuota)
	})

	t.Run("transfer then reversal", func(t *testing.T) {
		db, inviter, invitee, tracked := setup(t)
		wallet := inviter.Quota
		transfer, _, err := TransferAffiliateQuota(inviter.Id, "deduct-tracked-transfer")
		require.NoError(t, err)
		assert.Equal(t, 200, transfer.RewardQuota)
		var allocations []AffiliateRecord
		require.NoError(t, db.Where("kind = ?", "transfer_allocation").Find(&allocations).Error)
		require.Len(t, allocations, 1)
		assert.Equal(t, tracked.ID, allocations[0].RelatedRecordID)
		assert.Equal(t, 200, allocations[0].RewardQuota)

		_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, "deduct-tracked-subtract", nil)
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Equal(t, wallet+200, inviter.Quota)
		assert.Equal(t, 200, inviter.AffOffsetDebtQuota, "the transferred tracked 200 becomes debt once")
		var reversal AffiliateRecord
		require.NoError(t, db.Where("kind = ? AND related_record_id = ?", "reversal", tracked.ID).First(&reversal).Error)
		assert.Zero(t, reversal.CreditedQuota)
		assert.Equal(t, 200, reversal.DebtAddedQuota)
	})
}

// An administrator cannot deduct from a user of the same or a higher role,
// also when replaying; root can.
func TestDeductAffiliateQuotaRequiresAHigherRole(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	admin := User{Username: "deduct-admin-target", AffCode: "DEDUCT-ADMIN", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AffQuota: 500, AffHistoryQuota: 500}
	require.NoError(t, db.Create(&admin).Error)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": 500, "aff_history": 500}).Error)

	_, _, err := DeductAffiliateQuota(inviter.Id, common.RoleAdminUser, 100, "deduct-role-common")
	require.NoError(t, err, "an administrator may deduct from a common user")
	_, _, err = DeductAffiliateQuota(admin.Id, common.RoleAdminUser, 100, "deduct-role-admin")
	require.ErrorIs(t, err, ErrUserQuotaPermission)
	_, _, err = DeductAffiliateQuota(admin.Id, common.RoleRootUser, 100, "deduct-role-admin")
	require.NoError(t, err)
	_, _, err = DeductAffiliateQuota(admin.Id, common.RoleAdminUser, 100, "deduct-role-admin")
	require.ErrorIs(t, err, ErrUserQuotaPermission, "the permission check precedes the replay")
	var after User
	require.NoError(t, db.First(&after, admin.Id).Error)
	assert.Equal(t, 400, after.AffQuota)
}
