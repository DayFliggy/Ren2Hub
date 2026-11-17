package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// paidAffiliateTopUp settles a 10000 top-up for userID; at the 20% test rate it
// pays the inviter 2000.
func paidAffiliateTopUp(t *testing.T, userID int, trade string) (*TopUp, AffiliateRecord) {
	t.Helper()
	order := testAffiliateTopUp(t, userID, trade, 10000)
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	var accrual AffiliateRecord
	require.NoError(t, DB.Where("source_key = ?", topUpAffiliateKey(order.Id)).First(&accrual).Error)
	return order, accrual
}

func thawAffiliateForTest(t *testing.T, userID int) {
	t.Helper()
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		return thawAffiliateTx(tx, &user, common.GetTimestamp())
	}))
}

func countAffiliateRecords(t *testing.T, db *gorm.DB) int64 {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Count(&count).Error)
	return count
}

// A frozen reward is taken back from the frozen balance; when it falls due it
// thaws nothing. A reward that fell due before the reversal is thawed first
// and taken back from the available balance.
func TestReverseTopUpAffiliateRewardFrozenAndDue(t *testing.T) {
	t.Run("still frozen", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		operation_setting.GetAffiliateSetting().FreezeHours = 24
		order, accrual := paidAffiliateTopUp(t, invitee.Id, "reverse-frozen")
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		require.Equal(t, 2000, inviter.AffFrozenQuota)

		record, replayed, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.False(t, replayed)
		assert.Equal(t, 2000, record.RewardQuota)
		assert.Equal(t, 2000, record.CreditedQuota)
		assert.Zero(t, record.DebtAddedQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffFrozenQuota)
		assert.Zero(t, inviter.AffQuota)
		assert.Zero(t, inviter.AffOffsetDebtQuota)

		require.NoError(t, db.Model(&AffiliateRecord{}).Where("id = ?", accrual.ID).Update("frozen_until", common.GetTimestamp()-1).Error)
		thawAffiliateForTest(t, inviter.Id)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota, "the reversed reward thaws nothing")
		assert.Zero(t, inviter.AffFrozenQuota)
	})

	t.Run("due before the reversal", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		operation_setting.GetAffiliateSetting().FreezeHours = 24
		order, accrual := paidAffiliateTopUp(t, invitee.Id, "reverse-due")
		require.NoError(t, db.Model(&AffiliateRecord{}).Where("id = ?", accrual.ID).Update("frozen_until", common.GetTimestamp()-1).Error)

		record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.Equal(t, 2000, record.CreditedQuota)
		assert.Zero(t, record.DebtAddedQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffFrozenQuota)
		assert.Zero(t, inviter.AffQuota)
		assert.Zero(t, inviter.AffOffsetDebtQuota)
	})
}

// A thawed top-up reward is taken from the unattributed available balance;
// whatever was already transferred or deducted becomes debt that the next
// reward repays first.
func TestReverseTopUpAffiliateRewardAvailableTransferredAndPartial(t *testing.T) {
	t.Run("available", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-available")
		record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.Equal(t, 2000, record.CreditedQuota)
		assert.Zero(t, record.DebtAddedQuota)
		assert.Zero(t, record.AvailableAfter)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Zero(t, inviter.AffOffsetDebtQuota)
	})

	t.Run("transferred", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-transferred")
		_, _, err := TransferAffiliateQuota(inviter.Id, "reverse-transferred-out")
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		wallet := inviter.Quota

		record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.Zero(t, record.CreditedQuota)
		assert.Equal(t, 2000, record.DebtAddedQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Equal(t, 2000, inviter.AffOffsetDebtQuota)
		assert.Equal(t, wallet, inviter.Quota, "the wallet is handled separately")

		_, next := paidAffiliateTopUp(t, invitee.Id, "reverse-transferred-next")
		assert.Equal(t, 2000, next.RewardQuota)
		assert.Equal(t, 2000, next.DebtOffsetQuota, "the next reward repays the debt first")
		assert.Zero(t, next.CreditedQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffOffsetDebtQuota)
		assert.Zero(t, inviter.AffQuota)
	})

	t.Run("partly deducted", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-partial-one")
		paidAffiliateTopUp(t, invitee.Id, "reverse-partial-two")
		_, _, err := DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 3000, "reverse-partial-deduct")
		require.NoError(t, err)

		record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.Equal(t, 1000, record.CreditedQuota)
		assert.Equal(t, 1000, record.DebtAddedQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Equal(t, 1000, inviter.AffOffsetDebtQuota)
	})
}

// The part of the reward that repaid older debt is owed again.
func TestReverseTopUpAffiliateRewardRestoresRepaidDebt(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Update("aff_offset_debt_quota", 500).Error)
	order, accrual := paidAffiliateTopUp(t, invitee.Id, "reverse-offset")
	require.Equal(t, 500, accrual.DebtOffsetQuota)
	require.Equal(t, 1500, accrual.CreditedQuota)

	record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, 2000, record.RewardQuota)
	assert.Equal(t, 1500, record.CreditedQuota)
	assert.Equal(t, 500, record.DebtOffsetQuota)
	assert.Equal(t, 500, record.DebtAddedQuota)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Equal(t, 500, inviter.AffOffsetDebtQuota)
}

// Reward still attributed to an administrator addition is left alone, so the
// later administrator reversal and transfer of it stay consistent.
func TestReverseTopUpAffiliateRewardLeavesTrackedReward(t *testing.T) {
	setup := func(t *testing.T) (*gorm.DB, User, User, AffiliateRecord) {
		db, inviter, invitee := affiliateTestDB(t)
		operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
		_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "reverse-tracked-add", nil)
		require.NoError(t, err)
		order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-tracked-topup")
		var tracked AffiliateRecord
		require.NoError(t, db.Where("user_id = ? AND kind = ? AND reversible = ?", inviter.Id, "accrue", true).First(&tracked).Error)
		// Move all 2200 out, so the reversal finds no unattributed balance.
		_, _, err = TransferAffiliateQuota(inviter.Id, "reverse-tracked-out")
		require.NoError(t, err)
		// A second top-up brings 2000 back: 200 is no longer attributed to the
		// administrator addition, because the transfer took it.
		paidAffiliateTopUp(t, invitee.Id, "reverse-tracked-again")
		record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.Equal(t, 2000, record.CreditedQuota)
		assert.Equal(t, 2000, record.RewardQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		require.Zero(t, inviter.AffQuota)
		return db, inviter, invitee, tracked
	}

	t.Run("tracked reward untouched while available", func(t *testing.T) {
		db, inviter, invitee := affiliateTestDB(t)
		operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
		_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "reverse-keep-add", nil)
		require.NoError(t, err)
		order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-keep-topup")
		_, _, err = DeductAffiliateQuota(inviter.Id, common.RoleRootUser, 1500, "reverse-keep-deduct")
		require.NoError(t, err)
		// 700 left: 200 tracked and 500 unattributed.
		record, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
		require.NoError(t, err)
		assert.Equal(t, 500, record.CreditedQuota)
		assert.Equal(t, 1500, record.DebtAddedQuota)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, 200, inviter.AffQuota, "the tracked 200 stays")
		assert.Equal(t, 1500, inviter.AffOffsetDebtQuota)

		_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, "reverse-keep-subtract", nil)
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Equal(t, 1500, inviter.AffOffsetDebtQuota, "the administrator reversal takes the tracked 200 from the balance")
	})

	t.Run("administrator reversal after transfer", func(t *testing.T) {
		db, inviter, invitee, tracked := setup(t)
		var allocations []AffiliateRecord
		require.NoError(t, db.Where("kind = ?", "transfer_allocation").Find(&allocations).Error)
		require.Len(t, allocations, 1)
		assert.Equal(t, tracked.ID, allocations[0].RelatedRecordID)
		_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, "reverse-tracked-subtract", nil)
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Zero(t, inviter.AffQuota)
		assert.Equal(t, 200, inviter.AffOffsetDebtQuota, "only the transferred tracked 200 becomes debt")
	})

	t.Run("transfer after reversal", func(t *testing.T) {
		db, inviter, invitee, _ := setup(t)
		paidAffiliateTopUp(t, invitee.Id, "reverse-tracked-third")
		transfer, _, err := TransferAffiliateQuota(inviter.Id, "reverse-tracked-out-again")
		require.NoError(t, err)
		assert.Equal(t, 2000, transfer.RewardQuota)
		var allocations int64
		require.NoError(t, db.Model(&AffiliateRecord{}).Where("kind = ?", "transfer_allocation").Count(&allocations).Error)
		assert.EqualValues(t, 1, allocations, "a fungible transfer writes no allocation")
	})
}

// A replay returns the first record and changes nothing; a second order of the
// same invitee is reversed independently.
func TestReverseTopUpAffiliateRewardReplays(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-replay")
	paidAffiliateTopUp(t, invitee.Id, "reverse-replay-other")
	first, replayed, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
	require.NoError(t, err)
	require.False(t, replayed)
	records := countAffiliateRecords(t, db)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)

	again, replayed, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first.ID, again.ID)
	assert.Equal(t, first.CreditedQuota, again.CreditedQuota)
	var after User
	require.NoError(t, db.First(&after, inviter.Id).Error)
	assert.Equal(t, inviter.AffQuota, after.AffQuota)
	assert.Equal(t, inviter.AffOffsetDebtQuota, after.AffOffsetDebtQuota)
	assert.Equal(t, records, countAffiliateRecords(t, db))
	assert.Equal(t, 2000, after.AffQuota, "the other order's reward stays")
}

// Orders without a paid reward are rejected without writing anything.
func TestReverseTopUpAffiliateRewardRejectsOrdersWithoutReward(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	loner := User{Username: "reverse-loner", AffCode: "REVERSE-LONER", Status: common.UserStatusEnabled}
	require.NoError(t, db.Create(&loner).Error)
	_, err := RechargeEpay(testAffiliateTopUp(t, loner.Id, "reverse-no-inviter", 10000).TradeNo, "", "", "2.00")
	require.NoError(t, err)
	testAffiliateTopUp(t, invitee.Id, "reverse-unpaid", 10000)
	operation_setting.GetAffiliateSetting().Enabled = false
	_, skipped := paidAffiliateTopUp(t, invitee.Id, "reverse-skipped")
	require.Equal(t, "program_disabled", skipped.SkipReason)
	operation_setting.GetAffiliateSetting().Enabled = true
	paidAffiliateTopUp(t, invitee.Id, "reverse-gone-inviter")
	records := countAffiliateRecords(t, db)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	wantQuota := inviter.AffQuota

	for _, tc := range []struct {
		trade string
		err   error
	}{
		{"reverse-no-inviter", ErrAffiliateNoReward},
		{"reverse-unpaid", ErrAffiliateNoReward},
		{"reverse-skipped", ErrAffiliateNoReward},
		{"reverse-missing", nil},
	} {
		_, _, err := ReverseTopUpAffiliateReward(tc.trade, common.RoleRootUser)
		if tc.err != nil {
			assert.ErrorIs(t, err, tc.err, tc.trade)
		} else {
			assert.EqualError(t, err, "Top-up order does not exist", tc.trade)
		}
	}
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, wantQuota, inviter.AffQuota)
	assert.Equal(t, records, countAffiliateRecords(t, db))

	require.NoError(t, db.Unscoped().Delete(&User{}, inviter.Id).Error)
	_, _, err = ReverseTopUpAffiliateReward("reverse-gone-inviter", common.RoleRootUser)
	assert.ErrorIs(t, err, ErrAffiliateNoReward)
	assert.Equal(t, records, countAffiliateRecords(t, db))
}

// An administrator cannot reverse the reward of an inviter of the same or a
// higher role, also on replay; root can, including for a soft-deleted inviter.
func TestReverseTopUpAffiliateRewardRequiresAHigherRole(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Update("role", common.RoleAdminUser).Error)
	order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-role")
	records := countAffiliateRecords(t, db)
	_, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleAdminUser)
	require.ErrorIs(t, err, ErrUserQuotaPermission)
	assert.Equal(t, records, countAffiliateRecords(t, db))

	require.NoError(t, db.Delete(&User{}, inviter.Id).Error)
	_, replayed, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
	require.NoError(t, err)
	assert.False(t, replayed)
	require.NoError(t, db.Unscoped().First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota, "a soft-deleted inviter's balance is updated")

	_, _, err = ReverseTopUpAffiliateReward(order.TradeNo, common.RoleAdminUser)
	assert.ErrorIs(t, err, ErrUserQuotaPermission, "a replay checks the role too")
}

// Reversal keeps the lifetime reward and the per-invitee cap's gross count.
func TestReverseTopUpAffiliateRewardKeepsHistoryAndCap(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().PerInviteeCapQuota = 3000
	order, _ := paidAffiliateTopUp(t, invitee.Id, "reverse-cap-one")
	_, _, err := ReverseTopUpAffiliateReward(order.TradeNo, common.RoleRootUser)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 2000, inviter.AffHistoryQuota)

	_, next := paidAffiliateTopUp(t, invitee.Id, "reverse-cap-two")
	assert.Equal(t, 1000, next.RewardQuota, "the reversed 2000 still counts toward the cap")
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 3000, inviter.AffHistoryQuota)
	assert.Equal(t, 1000, inviter.AffQuota)
}
