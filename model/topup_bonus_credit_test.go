package model

import (
	"errors"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedTopUpBonus(bonus int, calls *int) TopUpBonusEvaluator {
	return func(int) (TopUpBonusGrant, error) {
		if calls != nil {
			*calls++
		}
		return TopUpBonusGrant{BonusQuota: bonus, ConfigVersion: 4, Decision: fmt.Sprintf(`{"bonus_quota":%d}`, bonus)}, nil
	}
}

func failingTopUpBonus(calls *int) TopUpBonusEvaluator {
	return func(int) (TopUpBonusGrant, error) {
		*calls++
		return TopUpBonusGrant{}, errors.New("bonus inputs unavailable")
	}
}

// An order freezes its full credit in ExpectedQuota and its principal as the
// referral base. Settlement reads nothing else, which is also what a release
// without bonus support does with the same row.
func TestTopUpBonusOrderCreditsFullAmountOnceWithPrincipalReferralBase(t *testing.T) {
	db, _, invitee := affiliateTestDB(t)
	require.NoError(t, db.AutoMigrate(&TopUpBonusDecisionRecord{}))
	order := TopUp{UserId: invitee.Id, TradeNo: "bonus-order", PaymentProvider: PaymentProviderEpay, PaymentMethod: "alipay",
		Status: common.TopUpStatusPending, ExpectedQuota: 1050, BonusQuota: 50, Money: 2, Currency: "CNY", PayAmount: "2.00",
		CreateTime: common.GetTimestamp(), Bonus: &TopUpBonusGrant{BonusQuota: 50, ConfigVersion: 4, Decision: `{"bonus_quota":50}`}}
	require.NoError(t, order.Insert())
	assert.Equal(t, 1000, order.AffiliateBaseQuota)

	for range 2 {
		_, err := RechargeEpay(order.TradeNo, "", "", "2.00")
		require.NoError(t, err)
	}
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 1050, invitee.Quota, "the bonus is credited with the principal, once")
	var stored TopUp
	require.NoError(t, db.First(&stored, order.Id).Error)
	assert.Equal(t, 1050, stored.CreditedQuota)
	assert.Equal(t, 50, stored.BonusQuota)
	var record AffiliateRecord
	require.NoError(t, db.First(&record, "source_key = ?", fmt.Sprintf("topup:%d", order.Id)).Error)
	assert.Equal(t, 1000, record.BaseQuota, "the bonus earns no referral reward")
	assert.Equal(t, 200, record.RewardQuota)
	decision, err := GetTopUpBonusDecision(TopUpBonusSourceTopUp, order.Id)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, invitee.Id, decision.UserID)
}

func TestFreezeTopUpQuotaRejectsBonusOutsideTheCredit(t *testing.T) {
	for _, order := range []TopUp{
		{PaymentProvider: PaymentProviderEpay, Amount: 1, BonusQuota: 10},
		{PaymentProvider: PaymentProviderEpay, ExpectedQuota: 100, BonusQuota: 100},
		{PaymentProvider: PaymentProviderEpay, ExpectedQuota: 100, BonusQuota: -1},
	} {
		require.ErrorIs(t, FreezeTopUpQuota(&order), ErrInvalidTopUpQuota)
	}
}

func TestRedeemCreditsBonusAndKeepsCodeOnFailure(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&TopUpBonusDecisionRecord{}))
	t.Cleanup(func() { DB.Exec("DELETE FROM topup_bonus_decisions") })
	userId, key := setupRedeemFixture(t, 500)

	calls := 0
	_, _, err := Redeem(key, userId, failingTopUpBonus(&calls))
	require.ErrorIs(t, err, ErrRedeemFailed)
	assert.Equal(t, 1, calls)
	_, _, err = Redeem(key, userId, fixedTopUpBonus(501, nil))
	require.ErrorIs(t, err, ErrRedeemFailed, "a bonus above the principal is rejected")
	var redemption Redemption
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	assert.Equal(t, common.RedemptionCodeStatusEnabled, redemption.Status, "a failed bonus keeps the code usable")

	quota, bonus, err := Redeem(key, userId, fixedTopUpBonus(25, nil))
	require.NoError(t, err)
	assert.Equal(t, 525, quota)
	assert.Equal(t, 25, bonus)
	var user User
	require.NoError(t, DB.First(&user, userId).Error)
	assert.Equal(t, 525, user.Quota)
	require.NoError(t, DB.First(&redemption, "key = ?", key).Error)
	assert.Equal(t, 500, redemption.Quota)
	assert.Equal(t, 25, redemption.BonusQuota)
	decision, err := GetTopUpBonusDecision(TopUpBonusSourceRedemption, redemption.Id)
	require.NoError(t, err)
	require.NotNil(t, decision)
	assert.Equal(t, userId, decision.UserID)
}

func TestAdminAdditionBonusIsDecidedOnceAndReplayed(t *testing.T) {
	db, _, invitee := affiliateTestDB(t)
	require.NoError(t, db.AutoMigrate(&TopUpBonusDecisionRecord{}))
	operation_setting.GetAffiliateSetting().AdminRechargeEnabled = true

	calls := 0
	result, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "bonus-add-one", fixedTopUpBonus(100, &calls))
	require.NoError(t, err)
	assert.Equal(t, 1100, result.After-result.Before)
	assert.Equal(t, 100, result.BonusQuota)
	assert.Equal(t, 1, calls)
	var operation AdminQuotaOperation
	require.NoError(t, db.First(&operation, "operation_id = ?", "bonus-add-one").Error)
	assert.Equal(t, 1000, operation.Value)
	assert.Equal(t, 100, operation.BonusQuota)
	var record AffiliateRecord
	require.NoError(t, db.First(&record, "source_key = ?", fmt.Sprintf("admin_quota:%d", operation.ID)).Error)
	assert.Equal(t, 1000, record.BaseQuota, "the bonus earns no referral reward")

	// A replay returns the stored bonus even when the bonus inputs fail now.
	failures := 0
	replay, err := AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "bonus-add-one", failingTopUpBonus(&failures))
	require.NoError(t, err)
	assert.True(t, replay.Replayed)
	assert.Equal(t, 100, replay.BonusQuota)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1001, "bonus-add-one", failingTopUpBonus(&failures))
	require.ErrorIs(t, err, ErrAffiliateOperationConflict)
	assert.Zero(t, failures, "replays and conflicts never decide a bonus")

	// A new addition fails with its bonus inputs and changes nothing.
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "add", 1000, "bonus-add-two", failingTopUpBonus(&failures))
	require.Error(t, err)
	assert.Equal(t, 1, failures)
	var count int64
	require.NoError(t, db.Model(&AdminQuotaOperation{}).Where("operation_id = ?", "bonus-add-two").Count(&count).Error)
	assert.Zero(t, count)

	// Subtractions and overrides never decide a bonus.
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "subtract", 100, "bonus-subtract", failingTopUpBonus(&failures))
	require.NoError(t, err)
	_, err = AdjustUserQuotaWithOperation(invitee.Id, 99, common.RoleRootUser, "override", 5000, "bonus-override", failingTopUpBonus(&failures))
	require.NoError(t, err)
	assert.Equal(t, 1, failures)
	require.NoError(t, db.First(&invitee, invitee.Id).Error)
	assert.Equal(t, 5000, invitee.Quota)
}
