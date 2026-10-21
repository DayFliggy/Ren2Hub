package model

import (
	"context"
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

func affiliateTestDB(t *testing.T) (*gorm.DB, User, User) {
	t.Helper()
	db := useGmpayTestDB(t)
	require.NoError(t, db.AutoMigrate(&AffiliateRecord{}, &AdminQuotaOperation{}))
	settings := operation_setting.GetAffiliateSetting()
	old := *settings
	*settings = operation_setting.AffiliateSetting{Enabled: true, RateBps: 2000}
	t.Cleanup(func() { *settings = old })
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

func TestAffiliateEveryProviderAndManualShareSuccessfulTransaction(t *testing.T) {
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
					return RechargeWithPayment(order.TradeNo, "customer", "", "USD", "1.00")
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
			require.NoError(t, credit())
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
	info, err := GetAffiliateInfo(inviter.Id)
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
func TestAffiliateAdminAdditionOperationAndCorrectionBoundaries(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500, "")
	require.ErrorIs(t, err, ErrAffiliateOperation)
	// Admin rebate is off by default, and enabling it later cannot reward a replay.
	result, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500, "admin-operation-one")
	require.NoError(t, err)
	assert.False(t, result.Replayed)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	result, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 500, "admin-operation-one")
	require.NoError(t, err)
	assert.True(t, result.Replayed)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 501, "admin-operation-one")
	require.ErrorIs(t, err, ErrAffiliateOperationConflict)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "admin-operation-two")
	require.NoError(t, err)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 100, "")
	require.NoError(t, err)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "override", 2000, "")
	require.NoError(t, err)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 200, inviter.AffQuota)
	assert.Equal(t, 2000, invitee.Quota)
	// Generic credit (including redemption) must never dispatch referral rewards.
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return creditTopUpQuota(tx, invitee.Id, 500, nil) }))
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Equal(t, 200, inviter.AffQuota)
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
			_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "same-admin-operation")
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
	assert.EqualValues(t, 2, count)
}
func TestAffiliatePerUserOverrideAndDisabledCodeOnlyAffectNewBindings(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	rate := 1234
	disabled := false
	custom := "CUSTOM-CODE"
	_, err := UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, AffiliatePolicyUpdate{RateBps: &rate, Enabled: &disabled, Code: &custom})
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
	_, err = UpdateAffiliateUserPolicy(inviter.Id, common.RoleRootUser, AffiliatePolicyUpdate{RateBps: nil})
	require.NoError(t, err)
	policy, err := GetAffiliateUserPolicy(inviter.Id, common.RoleRootUser)
	require.NoError(t, err)
	assert.Nil(t, policy.RateBps)
	assert.Equal(t, 2000, policy.EffectiveRateBps)
}
func TestAffiliateAdminStorageFailureRollsBackEntireAddition(t *testing.T) {
	db, inviter, invitee := affiliateTestDB(t)
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_admin_referral", func(tx *gorm.DB) {
		if tx.Statement.Table == "affiliate_records" {
			tx.AddError(errors.New("referral write failed"))
		}
	}))
	_, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "admin-failed-operation")
	require.Error(t, err)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	require.NoError(t, db.First(&inviter, inviter.Id).Error)
	assert.Zero(t, invitee.Quota)
	assert.Zero(t, inviter.AffQuota)
	var count int64
	require.NoError(t, db.Model(&AdminQuotaOperation{}).Count(&count).Error)
	assert.Zero(t, count)
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
