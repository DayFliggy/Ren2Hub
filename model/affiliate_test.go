package model

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func affiliateTestDB(t *testing.T) (*gorm.DB, User, User) {
	t.Helper()
	db := useGmpayTestDB(t)
	require.NoError(t, db.AutoMigrate(&AffiliateRecord{}, &AdminQuotaOperation{}))
	settings := operation_setting.GetAffiliateSetting()
	old := *settings
	*settings = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000}
	t.Cleanup(func() { *settings = old })
	payment := operation_setting.GetPaymentSetting()
	oldPayment := *payment
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() { *payment = oldPayment })
	inviter := User{Username: "inviter", AffCode: "INVITER", Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, db.Create(&inviter).Error)
	invitee := User{Username: "invitee", AffCode: "INVITEE", InviterId: inviter.Id, InvitedAt: common.GetTimestamp(), Status: common.UserStatusEnabled, Role: common.RoleCommonUser}
	require.NoError(t, db.Create(&invitee).Error)
	return db, inviter, invitee
}
func testAffiliateTopUp(t *testing.T, userID int, trade string, quota int) *TopUp {
	t.Helper()
	order := &TopUp{UserId: userID, TradeNo: trade, PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay", Status: common.TopUpStatusPending, ExpectedQuota: quota, Money: 2, Currency: "CNY", PayAmount: "2.00", CreateTime: common.GetTimestamp()}
	require.NoError(t, order.Insert())
	return order
}

func TestAffiliateNativeProviderFacadesAndManualShareSuccessfulTransaction(t *testing.T) {
	providers := []string{PaymentProviderEpay, PaymentProviderStripe, PaymentProviderCreem, PaymentProviderWaffo, PaymentProviderWaffoPancake, PaymentProviderGmpay}
	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			db, inviter, invitee := affiliateTestDB(t)
			currency := "USD"
			if provider == PaymentProviderEpay {
				currency = "CNY"
			}
			order := TopUp{UserId: invitee.Id, TradeNo: "provider-" + provider, PaymentProvider: provider, PaymentMethod: provider, Status: common.TopUpStatusPending, ExpectedQuota: 100003, Money: 1, Currency: currency, PayAmount: "1", Amount: 1, CreateTime: common.GetTimestamp()}
			var receipt GmpayPayment
			if provider == PaymentProviderGmpay {
				receipt = insertGmpaySnapshotTestOrder(t, &order, 100003, "test-key")
			} else {
				require.NoError(t, order.Insert())
			}
			credit := func() error {
				switch provider {
				case PaymentProviderEpay:
					_, err := RechargeEpay(order.TradeNo, "", "", "1.00")
					return err
				case PaymentProviderStripe:
					return Recharge(order.TradeNo, "customer", "")
				case PaymentProviderCreem:
					return RechargeCreem(order.TradeNo, "", "", "")
				case PaymentProviderWaffo:
					return RechargeWaffo(order.TradeNo, "")
				case PaymentProviderWaffoPancake:
					return RechargeWaffoPancake(order.TradeNo)
				default:
					_, err := RechargeGmpay(receipt, "")
					return err
				}
			}
			// Monetary unit settings changing after order creation cannot change credit.
			oldUnit := common.QuotaPerUnit
			common.QuotaPerUnit = 23
			t.Cleanup(func() { common.QuotaPerUnit = oldUnit })
			require.NoError(t, credit())
			replayErr := credit()
			if provider == PaymentProviderStripe || provider == PaymentProviderCreem {
				require.EqualError(t, replayErr, "top-up failed")
			} else {
				require.NoError(t, replayErr)
			}
			require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
			require.NoError(t, db.First(&invitee, invitee.Id).Error)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 100003, invitee.Quota)
			assert.Equal(t, 20000, inviter.AffQuota)
			assert.Equal(t, 20000, inviter.AffHistoryQuota)
			var count int64
			require.NoError(t, db.Model(&AffiliateRecord{}).Count(&count).Error)
			assert.EqualValues(t, 1, count)
			got := GetTopUpByTradeNo(order.TradeNo)
			assert.Equal(t, 100003, got.CreditedQuota)
			assert.Equal(t, 100003, got.AffiliateBaseQuota)
		})
	}
}
func TestAffiliateCallbackAdminCompletionConcurrentExactlyOnce(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	order := testAffiliateTopUp(t, invitee.Id, "concurrent-order", 500000)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
				errs <- err
			} else {
				errs <- ManualCompleteTopUp(order.TradeNo, "")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 500000, invitee.Quota)
	assert.Equal(t, 100000, inviter.AffQuota)
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}
func TestAffiliateFailureRollsBackWalletAndOrder(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	order := testAffiliateTopUp(t, invitee.Id, "rollback-order", 500000)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reject_referral", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_records" {
			tx.AddError(errors.New("simulated referral storage failure"))
		}
	}))
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.ErrorContains(t, err, "simulated referral")
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, invitee.Quota)
	assert.Zero(t, inviter.AffQuota)
	assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
	require.NoError(t, db.Callback().Create().Remove("reject_referral"))
	_, err = RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
}
func TestAffiliateSuccessTimeRulesFreezeCapAndExpiry(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	settings := operation_setting.GetAffiliateSetting()
	order := testAffiliateTopUp(t, invitee.Id, "policy-before-payment", 1000)
	settings.RateBps = 3333
	settings.FreezeHours = 1
	settings.PerInviteeCapQuota = 400
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	order2 := testAffiliateTopUp(t, invitee.Id, "policy-cap", 1000)
	_, err = RechargeEpay(order2.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Equal(t, 400, inviter.AffFrozenQuota)
	_, _, err = TransferAffiliateQuota(inviter.Id, "freeze-transfer")
	require.ErrorIs(t, err, ErrAffiliateUnavailable)
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ?", inviter.Id).Update("frozen_until", common.GetTimestamp()-1).Error)
	info, err := GetAffiliateInfo(inviter.Id, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, 400, info.AvailableReward)
	assert.Zero(t, info.FrozenReward)
	settings.PerInviteeCapQuota = 0
	settings.DurationDays = 1
	require.NoError(t, db.Model(&User{}).Where("id = ?", invitee.Id).Update("invited_at", common.GetTimestamp()-86401).Error)
	order3 := testAffiliateTopUp(t, invitee.Id, "policy-expired", 1000)
	_, err = RechargeEpay(order3.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 400, inviter.AffHistoryQuota)
}
func TestAffiliateFullTransferIsIdempotentAndKeepsLaterAccrual(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	order := testAffiliateTopUp(t, invitee.Id, "transfer-one", 500)
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	first, replayed, err := TransferAffiliateQuota(inviter.Id, "operation-transfer-1")
	require.NoError(t, err)
	assert.False(t, replayed)
	assert.Equal(t, 100, first.RewardQuota)
	order2 := testAffiliateTopUp(t, invitee.Id, "transfer-two", 250)
	_, err = RechargeEpay(order2.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	again, replayed, err := TransferAffiliateQuota(inviter.Id, "operation-transfer-1")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first.ID, again.ID)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 100, inviter.Quota)
	assert.Equal(t, 50, inviter.AffQuota)
	assert.Equal(t, 150, inviter.AffHistoryQuota)
}
func TestAffiliateTransferReplayAfterComplianceRevocation(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	firstOrder := testAffiliateTopUp(t, invitee.Id, "replay-before-revocation", 500)
	_, err := RechargeEpay(firstOrder.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	first, replayed, err := TransferAffiliateQuota(inviter.Id, "compliance-replay-transfer")
	require.NoError(t, err)
	require.False(t, replayed)
	require.Equal(t, 100, first.RewardQuota)
	laterOrder := testAffiliateTopUp(t, invitee.Id, "replay-later-accrual", 250)
	_, err = RechargeEpay(laterOrder.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = false
	payment.ComplianceTermsVersion = ""
	again, replayed, err := TransferAffiliateQuota(inviter.Id, "compliance-replay-transfer")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first, again, "a committed replay returns the original result, not current balances")
	_, replayed, err = TransferAffiliateQuota(inviter.Id, "compliance-new-transfer")
	require.ErrorIs(t, err, ErrAffiliateCompliance)
	assert.False(t, replayed)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 100, inviter.Quota)
	assert.Equal(t, 50, inviter.AffQuota)
	var transfers int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ?", inviter.Id, "transfer").Count(&transfers).Error)
	assert.EqualValues(t, 1, transfers)
}

func TestAffiliateTransferReplayPreservesReversalDebt(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "debt-replay-add", nil)
	require.NoError(t, err)
	first, replayed, err := TransferAffiliateQuota(inviter.Id, "debt-replay-transfer")
	require.NoError(t, err)
	require.False(t, replayed)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, "debt-replay-reverse", nil)
	require.NoError(t, err)
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = false
	again, replayed, err := TransferAffiliateQuota(inviter.Id, "debt-replay-transfer")
	require.NoError(t, err)
	assert.True(t, replayed)
	assert.Equal(t, first, again)
	payment.ComplianceConfirmed = true
	_, replayed, err = TransferAffiliateQuota(inviter.Id, "debt-replay-new-transfer")
	require.ErrorIs(t, err, ErrAffiliateUnavailable)
	assert.False(t, replayed)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 200, inviter.Quota)
	assert.Zero(t, inviter.AffQuota)
	assert.Equal(t, 200, inviter.AffOffsetDebtQuota, "replaying a receipt cannot erase reversal debt")
	var transfers int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("user_id = ? AND kind = ?", inviter.Id, "transfer").Count(&transfers).Error)
	assert.EqualValues(t, 1, transfers)
}

func TestAffiliateTransferRejectsConflictingRecordedOperation(t *testing.T) {
	for _, field := range []string{"user_id", "kind", "source_type", "source_id"} {
		t.Run(field, func(t *testing.T) {
			db, inviter, other := affiliateTestDB(t)
			require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Update("aff_quota", 100).Error)
			operationID := "conflicting-transfer-operation"
			record := AffiliateRecord{UserID: inviter.Id, Kind: "transfer", SourceType: "transfer", SourceID: operationID, SourceKey: fmt.Sprintf("transfer:%d:%s", inviter.Id, operationID), RewardQuota: 75}
			require.NoError(t, db.Create(&record).Error)
			var replacement any = "different-request"
			if field == "user_id" {
				replacement = other.Id
			}
			require.NoError(t, db.Model(&record).Update(field, replacement).Error)
			_, replayed, err := TransferAffiliateQuota(inviter.Id, operationID)
			require.ErrorIs(t, err, ErrAffiliateOperationConflict)
			assert.False(t, replayed)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Zero(t, inviter.Quota)
			assert.Equal(t, 100, inviter.AffQuota)
		})
	}
}

func TestAffiliateTransferOperationIDsAreScopedToOwner(t *testing.T) {
	db, firstOwner, secondOwner := affiliateTestDB(t)
	for _, owner := range []*User{&firstOwner, &secondOwner} {
		require.NoError(t, db.Model(owner).Update("aff_quota", owner.Id*100).Error)
	}
	first, replayed, err := TransferAffiliateQuota(firstOwner.Id, "shared-transfer-operation")
	require.NoError(t, err)
	require.False(t, replayed)
	second, replayed, err := TransferAffiliateQuota(secondOwner.Id, "shared-transfer-operation")
	require.NoError(t, err)
	require.False(t, replayed)
	assert.NotEqual(t, first.ID, second.ID)
	assert.Equal(t, firstOwner.Id*100, first.RewardQuota)
	assert.Equal(t, secondOwner.Id*100, second.RewardQuota)
	for _, owner := range []*User{&firstOwner, &secondOwner} {
		require.NoError(t, db.First(owner, owner.Id).Error)
		assert.Equal(t, owner.Id*100, owner.Quota)
		assert.Zero(t, owner.AffQuota)
	}
}

func TestAffiliateAdminAdditionOperationAndCorrectionBoundaries(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500, "", nil)
	require.ErrorIs(t, err, ErrAffiliateOperation)
	// Admin rebate is off by default, and enabling it later cannot reward a replay.
	result, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500, "admin-operation-one", nil)
	require.NoError(t, err)
	assert.False(t, result.Replayed)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	result, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500, "admin-operation-one", nil)
	require.NoError(t, err)
	assert.True(t, result.Replayed)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 501, "admin-operation-one", nil)
	require.ErrorIs(t, err, ErrAffiliateOperationConflict)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "admin-operation-two", nil)
	require.NoError(t, err)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 100, "admin-operation-three", nil)
	require.NoError(t, err)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "override", 2000, "admin-operation-four", nil)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 300, inviter.AffQuota)
	assert.Equal(t, 2000, invitee.Quota)
	// Correcting the same operation under another mode is a conflict, not a replay.
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "admin-operation-four", nil)
	require.ErrorIs(t, err, ErrAffiliateOperationConflict)
	// Generic credit (including redemption) must never dispatch referral rewards.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return creditTopUpQuota(tx, invitee.Id, 500, nil) }))
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 300, inviter.AffQuota)
}

func TestAffiliateAdminReversalFIFOTransferredDebtAndFutureOffset(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	for _, operationID := range []string{"admin-fifo-one", "admin-fifo-two"} {
		_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, operationID, nil)
		require.NoError(t, err)
	}
	// The first full reward and half of the second are revoked in FIFO order.
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1500, "admin-fifo-reduction-one", nil)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 100, inviter.AffQuota)
	firstTransfer, replay, err := TransferAffiliateQuota(inviter.Id, "affiliate-early-transfer")
	require.NoError(t, err)
	assert.False(t, replay)
	assert.Equal(t, 100, firstTransfer.RewardQuota)
	// Two partial reversals of already-transferred reward must each charge only
	// the portion not yet reversed, without subtracting the transfer twice.
	for _, operationID := range []string{"admin-fifo-reduction-two", "admin-fifo-reduction-three"} {
		_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 250, operationID, nil)
		require.NoError(t, err)
	}
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	assert.Equal(t, 100, inviter.AffOffsetDebtQuota)
	// New rewards first repay that debt and do not become transferable.
	order := testAffiliateTopUp(t, invitee.Id, "admin-reversal-offset", 500)
	_, err = RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffOffsetDebtQuota)
	assert.Zero(t, inviter.AffQuota)
	var offset AffiliateRecord
	require.NoError(t, db.Where("source_key = ?", "topup:"+fmt.Sprint(order.Id)).First(&offset).Error)
	assert.Equal(t, 100, offset.RewardQuota)
	assert.Equal(t, 100, offset.DebtOffsetQuota)
	assert.Zero(t, offset.CreditedQuota)
	var reversals []AffiliateRecord
	require.NoError(t, db.Where("kind = ?", "reversal").Order("id ASC").Find(&reversals).Error)
	require.Len(t, reversals, 4)
	assert.Equal(t, 50, reversals[2].DebtAddedQuota)
	assert.Equal(t, 0, reversals[2].CreditedQuota)
	assert.Equal(t, 50, reversals[3].DebtAddedQuota)
	// No more tracked administrative principal remains: the extra reduction
	// cannot invent new referral debt, including after a later top-up.
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 600, "admin-fifo-excess-reduction", nil)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffOffsetDebtQuota)
}

func TestAffiliateComplianceGateAndTrackedLegacyBoundary(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	payment := operation_setting.GetPaymentSetting()
	payment.ComplianceConfirmed = false
	payment.ComplianceTermsVersion = ""
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "compliance-gated-add", nil)
	require.NoError(t, err, "wallet credit must still settle while referral accrual is recorded as skipped")
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	var skipped AffiliateRecord
	require.NoError(t, db.Where("source_type = ?", "admin_quota").First(&skipped).Error)
	assert.Equal(t, "compliance_unconfirmed", skipped.SkipReason)
	_, _, err = TransferAffiliateQuota(inviter.Id, "compliance-transfer")
	require.ErrorIs(t, err, ErrAffiliateCompliance)

	// Simulate pre-migration data: old add rows and their earned balance remain
	// untouched by a later correction because they have no transfer attribution.
	payment.ComplianceConfirmed = true
	payment.ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	legacyInvitee := User{Username: "legacy-invitee", InviterId: inviter.Id, Quota: 1000}
	require.NoError(t, db.Create(&legacyInvitee).Error)
	legacyOp := AdminQuotaOperation{OperatorID: 77, OperationID: "legacy-admin-add", OperationKey: "77:legacy-admin-add", UserID: legacyInvitee.Id, Value: 1000, Before: 0, After: 1000}
	require.NoError(t, db.Create(&legacyOp).Error)
	legacyReward := AffiliateRecord{UserID: inviter.Id, InviteeID: legacyInvitee.Id, Kind: "accrue", SourceType: "admin_quota", SourceID: fmt.Sprint(legacyOp.ID), SourceKey: "admin_quota:" + fmt.Sprint(legacyOp.ID), BaseQuota: 1000, RateBps: 2000, RewardQuota: 200, ThawedAt: common.GetTimestamp()}
	require.NoError(t, db.Create(&legacyReward).Error)
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": 200, "aff_history": 200}).Error)
	replayed, err := AdjustUserQuotaWithOperation(legacyInvitee.Id, 77, common.RoleRootUser, "add", 1000, "legacy-admin-add", nil)
	require.NoError(t, err)
	assert.True(t, replayed.Replayed, "legacy operation rows without mode remain replayable as add operations")
	_, err = AdjustUserQuotaWithOperation(legacyInvitee.Id, 99, common.RoleRootUser, "subtract", 500, "legacy-admin-correction", nil)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 200, inviter.AffQuota)
	assert.Zero(t, inviter.AffOffsetDebtQuota)
	// Historical transfers have no allocation rows. Only the current legacy
	// balance may move; past gross accrual must not be treated as available.
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Update("aff_quota", 50).Error)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "post-migration-admin-add", nil)
	require.NoError(t, err)
	transfer, _, err := TransferAffiliateQuota(inviter.Id, "post-migration-transfer")
	require.NoError(t, err)
	assert.Equal(t, 250, transfer.RewardQuota)
	var allocations []AffiliateRecord
	require.NoError(t, db.Where("kind = ?", "transfer_allocation").Find(&allocations).Error)
	require.Len(t, allocations, 1)
	assert.Equal(t, 200, allocations[0].RewardQuota)
}

func TestAffiliateReversalRestoresOffsetDebtBeforeNewCredit(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "debt-origin-add", nil)
	require.NoError(t, err)
	_, _, err = TransferAffiliateQuota(inviter.Id, "debt-origin-transfer")
	require.NoError(t, err)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 1000, "debt-origin-reversal", nil)
	require.NoError(t, err)
	// 200 of the next 300 reward offsets existing debt; 100 becomes available.
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1500, "debt-repaying-add", nil)
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffOffsetDebtQuota)
	assert.Equal(t, 100, inviter.AffQuota)
	// Reverse the repayment in two steps and then its credited remainder.
	for _, tc := range []struct{ value, debt, available int }{{500, 100, 100}, {500, 200, 100}, {500, 200, 0}} {
		_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", tc.value, fmt.Sprintf("debt-repayment-reversal-%d-%d", tc.debt, tc.available), nil)
		require.NoError(t, err)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, tc.debt, inviter.AffOffsetDebtQuota)
		assert.Equal(t, tc.available, inviter.AffQuota)
	}
}

func TestEnsureAffiliateCodeBackfillsLegacyUserAndMigrationKeepsBalance(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	legacy := User{Username: "legacy-empty-code", AffQuota: 456, AffHistoryQuota: 789}
	require.NoError(t, db.Create(&legacy).Error)
	// Exercise the additive schema path against data that predates debt tracking.
	require.NoError(t, db.Migrator().DropColumn(&User{}, "aff_offset_debt_quota"))
	require.NoError(t, db.AutoMigrate(&User{}))
	var migrated User
	require.NoError(t, db.First(&migrated, legacy.Id).Error)
	assert.Equal(t, 456, migrated.AffQuota)
	assert.Equal(t, 789, migrated.AffHistoryQuota)
	assert.Zero(t, migrated.AffOffsetDebtQuota)
	code, err := EnsureAffiliateCode(legacy.Id)
	require.NoError(t, err)
	assert.Regexp(t, affiliateCodePattern, code)
	second, err := EnsureAffiliateCode(legacy.Id)
	require.NoError(t, err)
	assert.Equal(t, code, second)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffCount, "backfilling a user's own invitation code must not create referral attribution")
}
func TestAffiliateRegistrationFailureAndBindingAtomic(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	for i, code := range []string{"MISSING", "INVALID!"} {
		user := User{Username: fmt.Sprintf("invalid-%d", i)}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := InitializeAffiliateUserTx(tx, &user, code); err != nil {
				return err
			}
			return tx.Create(&user).Error
		})
		require.ErrorIs(t, err, ErrAffiliateCodeInvalid)
	}
	user := User{Username: "registered"}
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := InitializeAffiliateUserTx(tx, &user, "inviter"); err != nil {
			return err
		}
		return tx.Create(&user).Error
	})
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 1, inviter.AffCount)
	assert.Equal(t, inviter.Id, user.InviterId)
	assert.Positive(t, user.InvitedAt)
	assert.NotEmpty(t, user.AffCode)
	assert.Zero(t, inviter.AffQuota)
	// A downstream registration failure rolls back the invitation count too.
	err = db.Transaction(func(tx *gorm.DB) error {
		candidate := User{}
		if err := InitializeAffiliateUserTx(tx, &candidate, "INVITER"); err != nil {
			return err
		}
		return errors.New("provider binding failed")
	})
	require.Error(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 1, inviter.AffCount)
	operation_setting.GetAffiliateSetting().Enabled = false
	err = db.Transaction(func(tx *gorm.DB) error { return InitializeAffiliateUserTx(tx, &User{}, "INVITER") })
	require.ErrorIs(t, err, ErrAffiliateDisabled)
	err = db.Transaction(func(tx *gorm.DB) error { return InitializeAffiliateUserTx(tx, &User{}, "") })
	require.NoError(t, err)
}

func TestAffiliateDebtIsNotOverwrittenByStaleProfileUpdate(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	inviter.AffOffsetDebtQuota = 100
	inviter.DisplayName = "Updated profile"
	require.NoError(t, db.Model(&User{}).Where("id = ?", inviter.Id).Update("aff_offset_debt_quota", 200).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return inviter.UpdateWithTx(tx, false) }))
	var current User
	require.NoError(t, db.First(&current, inviter.Id).Error)
	assert.Equal(t, "Updated profile", current.DisplayName)
	assert.Equal(t, 200, current.AffOffsetDebtQuota, "profile writes must preserve financial state committed since their snapshot")
}

func TestAffiliateGmpayValidationPrecedesCredit(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	order := TopUp{UserId: invitee.Id, TradeNo: "gmpay-invalid", PaymentProvider: PaymentProviderGmpay, Status: common.TopUpStatusPending}
	receipt := insertGmpaySnapshotTestOrder(t, &order, 500, "private-test")
	receipt.AmountMinor++
	_, err := RechargeGmpay(receipt, "")
	require.ErrorIs(t, err, ErrTopUpReceiptMismatch)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, inviter.AffQuota)
	require.Error(t, CreateGmpayOrder(context.Background(), nil, nil, nil))
}

func TestAffiliateConcurrentAdminAndTransferOperations(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	var wg sync.WaitGroup
	errorsOut := make(chan error, 24)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "same-admin-operation", nil)
			errorsOut <- err
		}()
	}
	wg.Wait()
	for i := 0; i < 12; i++ {
		require.NoError(t, <-errorsOut)
	}
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 1000, invitee.Quota)
	assert.Equal(t, 200, inviter.AffQuota)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := TransferAffiliateQuota(inviter.Id, "same-transfer-operation")
			errorsOut <- err
		}()
	}
	wg.Wait()
	for i := 0; i < 12; i++ {
		require.NoError(t, <-errorsOut)
	}
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 200, inviter.Quota)
	assert.Zero(t, inviter.AffQuota)
	var count int64
	require.NoError(t, db.Model(&AdminQuotaOperation{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
	require.NoError(t, db.Model(&AffiliateRecord{}).Count(&count).Error)
	assert.EqualValues(t, 3, count)
}
func TestAffiliatePerUserOverrideAndDisabledCodeOnlyAffectNewBindings(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	rate := 1234
	disabled := false
	custom := "CUSTOM-CODE"
	_, err := UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, AffiliatePolicyUpdate{RateBps: AffiliateRateUpdate{Set: true, Value: &rate}, Enabled: &disabled, Code: &custom})
	require.NoError(t, err)
	order := testAffiliateTopUp(t, invitee.Id, "disabled-code-existing-binding", 10000)
	_, err = RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 1234, inviter.AffQuota)
	err = db.Transaction(func(tx *gorm.DB) error { return InitializeAffiliateUserTx(tx, &User{}, custom) })
	require.ErrorIs(t, err, ErrAffiliateCodeInvalid)
	_, err = UpdateAffiliateUserPolicy(inviter.Id, common.RoleCommonUser, AffiliatePolicyUpdate{})
	require.ErrorIs(t, err, ErrUserQuotaPermission)
	_, err = UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, AffiliatePolicyUpdate{RateBps: AffiliateRateUpdate{Set: true}})
	require.NoError(t, err)
	policy, err := GetAffiliateUserPolicy(inviter.Id, common.RoleRootUser)
	require.NoError(t, err)
	assert.Nil(t, policy.RateBps)
	assert.Equal(t, 2000, policy.EffectiveRateBps)
}

// 只提交 enabled 或 code 时不能清空专属比例；显式 null 才清除。
func TestAffiliatePolicyUpdateKeepsUnsubmittedFields(t *testing.T) {
	db, inviter, _ := affiliateTestDB(t)
	decode := func(body string) AffiliatePolicyUpdate {
		var request AffiliatePolicyUpdate
		require.NoError(t, common.Unmarshal([]byte(body), &request))
		return request
	}
	_, err := UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, decode(`{"rate_bps":1500}`))
	require.NoError(t, err)
	for _, body := range []string{`{"enabled":false}`, `{"code":"KEEP-RATE"}`, `{}`} {
		policy, err := UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, decode(body))
		require.NoError(t, err, body)
		require.NotNil(t, policy.RateBps, body)
		assert.Equal(t, 1500, *policy.RateBps, body)
	}
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.False(t, inviter.AffCodeEnabled)
	assert.Equal(t, "KEEP-RATE", inviter.AffCode)
	policy, err := UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, decode(`{"rate_bps":null,"enabled":true}`))
	require.NoError(t, err)
	assert.Nil(t, policy.RateBps)
	assert.True(t, policy.Enabled)
	assert.Equal(t, "KEEP-RATE", policy.Code)
	_, err = UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, decode(`{"rate_bps":10001}`))
	assert.Error(t, err)
}
func TestAffiliateAdminStorageFailureRollsBackEntireAddition(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_admin_referral", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_records" {
			tx.AddError(errors.New("referral write failed"))
		}
	}))
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "admin-failed-operation", nil)
	require.Error(t, err)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, invitee.Quota)
	assert.Zero(t, inviter.AffQuota)
	var count int64
	require.NoError(t, db.Model(&AdminQuotaOperation{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestAffiliateAdminReversalStorageFailureRollsBackWalletAndDebt(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "rollback-origin-add", nil)
	require.NoError(t, err)
	_, _, err = TransferAffiliateQuota(inviter.Id, "rollback-origin-transfer")
	require.NoError(t, err)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_reversal_ledger", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_records" {
			tx.AddError(errors.New("reversal ledger unavailable"))
		}
	}))
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 500, "rollback-failed-reduction", nil)
	require.ErrorContains(t, err, "reversal ledger unavailable")
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 1000, invitee.Quota)
	assert.Equal(t, 200, inviter.Quota)
	assert.Zero(t, inviter.AffOffsetDebtQuota)
	var count int64
	require.NoError(t, db.Model(&AdminQuotaOperation{}).Where("operation_id = ?", "rollback-failed-reduction").Count(&count).Error)
	assert.Zero(t, count)
	var ledgerCount int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Where("kind = ?", "reversal").Count(&ledgerCount).Error)
	assert.Zero(t, ledgerCount)
}
func TestTopUpTerminalStatusCannotRevertOrSkipSettlement(t *testing.T) {
	_, _, invitee := affiliateTestDB(t)
	order := testAffiliateTopUp(t, invitee.Id, "terminal-order", 100)
	require.ErrorIs(t, UpdatePendingTopUpStatus(order.TradeNo, PaymentProviderEpay, common.TopUpStatusSuccess), ErrTopUpStatusInvalid)
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	order.Status = common.TopUpStatusFailed
	require.ErrorIs(t, order.Update(), ErrTopUpStatusInvalid)
	assert.Equal(t, common.TopUpStatusSuccess, GetTopUpByTradeNo(order.TradeNo).Status)
}

func TestAffiliateSkippedDecisionIsImmutableForEachSource(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		base      int
		configure func(*testing.T, *gorm.DB, *User, *User, *operation_setting.AffiliateSetting)
	}{
		{name: "program_disabled", source: "topup", base: 1000, configure: func(t *testing.T, db *gorm.DB, inviter, invitee *User, policy *operation_setting.AffiliateSetting) {
			policy.Enabled = false
		}},
		{name: "admin_recharge_disabled", source: "admin_quota", base: 1000, configure: func(t *testing.T, db *gorm.DB, inviter, invitee *User, policy *operation_setting.AffiliateSetting) {
			policy.AdminRechargeEnabled = false
		}},
		{name: "no_paid_quota", source: "topup", base: 0},
		{name: "referral_expired", source: "topup", base: 1000, configure: func(t *testing.T, db *gorm.DB, inviter, invitee *User, policy *operation_setting.AffiliateSetting) {
			policy.DurationDays = 1
			require.NoError(t, db.Model(invitee).Update("invited_at", common.GetTimestamp()-86401).Error)
		}},
		{name: "inviter_disabled", source: "topup", base: 1000, configure: func(t *testing.T, db *gorm.DB, inviter, invitee *User, policy *operation_setting.AffiliateSetting) {
			require.NoError(t, db.Model(inviter).Update("status", common.UserStatusDisabled).Error)
		}},
		{name: "zero_rate", source: "topup", base: 1000, configure: func(t *testing.T, db *gorm.DB, inviter, invitee *User, policy *operation_setting.AffiliateSetting) {
			policy.RateBps = 0
		}},
		{name: "below_quota_unit", source: "topup", base: 1},
		{name: "invitee_cap_reached", source: "topup", base: 1000, configure: func(t *testing.T, db *gorm.DB, inviter, invitee *User, policy *operation_setting.AffiliateSetting) {
			policy.PerInviteeCapQuota = 100
			require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, InviteeID: invitee.Id, Kind: "accrue", SourceKey: "previous", RewardQuota: 100}).Error)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, inviter, invitee := affiliateTestDB(t)
			policy := operation_setting.GetAffiliateSetting()
			if tc.configure != nil {
				tc.configure(t, db, &inviter, &invitee, policy)
			}
			apply := func() error {
				return db.Transaction(func(tx *gorm.DB) error {
					user, err := lockAffiliateParticipantsTx(tx, invitee.Id)
					if err != nil {
						return err
					}
					return applyAffiliateRewardTx(tx, user, tc.source, "immutable", tc.base)
				})
			}
			require.NoError(t, apply())
			var recorded AffiliateRecord
			require.NoError(t, db.Where("source_key = ?", tc.source+":immutable").First(&recorded).Error)
			assert.Equal(t, tc.name, recorded.SkipReason)
			assert.Equal(t, tc.base, recorded.BaseQuota)
			assert.Equal(t, policy.RateBps, recorded.RateBps)
			assert.Zero(t, recorded.RewardQuota)
			assert.Zero(t, recorded.FrozenUntil)
			*policy = operation_setting.AffiliateSetting{Enabled: true, AdminRechargeEnabled: true, RateBps: 10000}
			require.NoError(t, db.Model(&inviter).Update("status", common.UserStatusEnabled).Error)
			require.NoError(t, db.Model(&invitee).Update("invited_at", common.GetTimestamp()).Error)
			require.NoError(t, apply())
			var replay AffiliateRecord
			require.NoError(t, db.Where("source_key = ?", tc.source+":immutable").First(&replay).Error)
			assert.Equal(t, recorded, replay)
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Zero(t, inviter.AffQuota)
			assert.Zero(t, inviter.AffFrozenQuota)
		})
	}
}

func TestAffiliateDisabledOrderPersistsSkipAndNeverBackfills(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().Enabled = false
	order := testAffiliateTopUp(t, invitee.Id, "disabled-paid-order", 1000)
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	var record AffiliateRecord
	require.NoError(t, db.Where("source_key = ?", fmt.Sprintf("topup:%d", order.Id)).First(&record).Error)
	assert.Equal(t, "program_disabled", record.SkipReason)
	assert.Equal(t, 1000, record.BaseQuota)
	assert.Equal(t, 2000, record.RateBps)
	operation_setting.GetAffiliateSetting().Enabled = true
	require.NoError(t, ManualCompleteTopUp(order.TradeNo, ""))
	_, err = RechargeEpay(order.TradeNo, "", "", "2.00")
	require.NoError(t, err)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 1000, invitee.Quota)
	assert.Zero(t, inviter.AffQuota)
	var count int64
	require.NoError(t, db.Model(&AffiliateRecord{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

func TestAffiliateSkipStorageFailureRollsBackPaidOrder(t *testing.T) {
	db, _, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().Enabled = false
	order := testAffiliateTopUp(t, invitee.Id, "skip-rollback", 1000)
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("reject_skip", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_records" {
			tx.AddError(errors.New("skip decision storage unavailable"))
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove("reject_skip") })
	_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
	require.ErrorContains(t, err, "skip decision storage unavailable")
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Zero(t, invitee.Quota)
	assert.Equal(t, common.TopUpStatusPending, GetTopUpByTradeNo(order.TradeNo).Status)
}

func TestAffiliateInviteesMaskedOwnedAndBounded(t *testing.T) {
	db, inviter, first := affiliateTestDB(t)
	require.NoError(t, db.Model(&first).Update("invited_at", 100).Error)
	second := User{AffCode: "SECOND01", Username: "private.second", Email: "not-public@example.test", InviterId: inviter.Id, InvitedAt: 200}
	foreign := User{AffCode: "FOREIGN1", Username: "foreign", InviterId: first.Id, InvitedAt: 300}
	require.NoError(t, db.Create(&second).Error)
	require.NoError(t, db.Create(&foreign).Error)
	require.NoError(t, db.Create(&AffiliateRecord{UserID: inviter.Id, InviteeID: second.Id, Kind: "accrue", SourceKey: "own-invitee", RewardQuota: 123}).Error)
	require.NoError(t, db.Create(&AffiliateRecord{UserID: first.Id, InviteeID: second.Id, Kind: "accrue", SourceKey: "foreign-reward", RewardQuota: 999}).Error)
	page := &common.PageInfo{Page: 1, PageSize: 1}
	rows, total, err := ListAffiliateInvitees(inviter.Id, "", page)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.EqualValues(t, 2, total)
	assert.EqualValues(t, 200, rows[0].InvitedAt)
	assert.Equal(t, "p***d", rows[0].MaskedName)
	assert.Equal(t, 123, rows[0].RewardQuota)
	page.Page = 2
	rows, _, err = ListAffiliateInvitees(inviter.Id, "desc", page)
	require.NoError(t, err)
	assert.EqualValues(t, 100, rows[0].InvitedAt)
	page.Page = 1
	rows, _, err = ListAffiliateInvitees(inviter.Id, "asc", page)
	require.NoError(t, err)
	assert.EqualValues(t, 100, rows[0].InvitedAt)
	page.Page = 3
	rows, _, err = ListAffiliateInvitees(inviter.Id, "desc", page)
	require.NoError(t, err)
	assert.Empty(t, rows)
	for _, bad := range []*common.PageInfo{nil, {Page: 0, PageSize: 1}, {Page: 1, PageSize: -1}, {Page: 1, PageSize: 101}} {
		_, _, err = ListAffiliateInvitees(inviter.Id, "", bad)
		require.Error(t, err)
	}
	_, _, err = ListAffiliateInvitees(inviter.Id, "id;", &common.PageInfo{Page: 1, PageSize: 20})
	require.Error(t, err)
	assert.Equal(t, "用***", maskAffiliateName("用户"))
	assert.Equal(t, "***", maskAffiliateName(""))
}
