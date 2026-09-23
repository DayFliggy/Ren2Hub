package model

import (
	"errors"
	"math"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useWalletIngressDB(t *testing.T) *gorm.DB {
	t.Helper()
	oldDB, oldLogDB, oldRedis, oldType := DB, LOG_DB, common.RedisEnabled, common.MainDatabaseType()
	oldKeyCol, oldUnit := commonKeyCol, common.QuotaPerUnit
	commonKeyCol, common.QuotaPerUnit = "`key`", 500000
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}, &Token{}, &BillingRecovery{}, &BillingEvent{}, &TopUp{}, &EpayOrder{}, &EpayMerchantVersion{}, &GmpayOrder{}, &GmpayMerchantVersion{}, &Option{}, &Redemption{}, &Checkin{}, &ActivityClaim{}, &AffiliateLedger{}, &UserRouteProfile{}, &Log{}))
	DB, LOG_DB, common.RedisEnabled = db, db, false
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() {
		DB, LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis
		commonKeyCol, common.QuotaPerUnit = oldKeyCol, oldUnit
		common.SetMainDatabaseType(oldType)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestWalletIngressTopUpIsAtomicAndBeyondInt32(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "payer", AffCode: "payer", Quota: math.MaxInt32}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "payment-1", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, Amount: 5000}
	payment := insertEpaySnapshotTestOrder(t, &order, 2_500_000_000, "test-key")
	already, err := RechargeEpay(payment, "")
	require.NoError(t, err)
	assert.False(t, already)
	already, err = RechargeEpay(payment, "")
	require.NoError(t, err)
	assert.True(t, already)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.Quota(math.MaxInt32)+2500000000, user.Quota)
	var count int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
	var event BillingEvent
	require.NoError(t, db.First(&event).Error)
	assert.Equal(t, "payment-1", event.SourceID)
	assert.Equal(t, common.Quota(2500000000), event.AvailableDelta)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		err := creditTopUpQuota(tx, &order, 1, nil)
		assert.ErrorIs(t, err, ErrBillingEventConflict)
		return nil
	}))
}

func TestWalletIngressOverflowRollsBackRewardClaim(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "full-wallet", AffCode: "full-wallet", Quota: math.MaxInt64}
	require.NoError(t, db.Create(&user).Error)
	require.ErrorIs(t, ClaimActivityReward(user.Id, "overflow", 1), common.ErrQuotaOverflow)
	var count int64
	require.NoError(t, db.Model(&ActivityClaim{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.Model(&BillingEvent{}).Count(&count).Error)
	assert.Zero(t, count)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, common.Quota(math.MaxInt64), user.Quota)
}

func TestWalletIngressCheckinRollbackOnEventFailure(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "checkin", AffCode: "checkin"}
	require.NoError(t, db.Create(&user).Error)
	injected := errors.New("event insert failed")
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_ingress_event", func(tx *gorm.DB) {
		if tx.Statement.Table == "billing_events" {
			tx.AddError(injected)
		}
	}))
	t.Cleanup(func() { db.Callback().Create().Remove("fail_ingress_event") })
	_, err := userCheckinWithTransaction(&Checkin{UserId: user.Id, CheckinDate: "2026-09-19", QuotaAwarded: 100}, user.Id, 100)
	require.ErrorIs(t, err, injected)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Zero(t, user.Quota)
	var count int64
	require.NoError(t, db.Model(&Checkin{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestWalletIngressRegistrationRewardsHaveEvents(t *testing.T) {
	db := useWalletIngressDB(t)
	oldNew, oldInvitee, oldInviter := common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter
	oldPayment := *operation_setting.GetPaymentSetting()
	common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter = 100, 25, 0
	operation_setting.GetPaymentSetting().ComplianceConfirmed = true
	operation_setting.GetPaymentSetting().ComplianceTermsVersion = operation_setting.CurrentComplianceTermsVersion
	t.Cleanup(func() {
		common.QuotaForNewUser, common.QuotaForInvitee, common.QuotaForInviter = oldNew, oldInvitee, oldInviter
		*operation_setting.GetPaymentSetting() = oldPayment
	})
	inviter := User{Username: "inviter", AffCode: "inviter"}
	require.NoError(t, db.Create(&inviter).Error)
	user := User{Username: "new-user"}
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return user.InsertWithTx(tx, inviter.Id) }))
	assert.Equal(t, common.Quota(125), user.Quota)
	var events []BillingEvent
	require.NoError(t, db.Where("user_id = ?", user.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Equal(t, "registration", events[0].SourceType)
	assert.Equal(t, common.Quota(100), events[0].AvailableDelta)
	assert.Equal(t, common.Quota(25), events[1].AvailableDelta)
}

func TestWalletIngressManagedAdjustmentRequiresVersionAndReplays(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "managed", AffCode: "managed", Role: common.RoleCommonUser}
	require.NoError(t, db.Create(&user).Error)
	version := int64(0)
	input := WalletAdjustment{EventID: "admin:operation-1", UserID: user.Id, ActorID: 100, Delta: math.MaxInt32 + 1, SourceID: "operation-1", ExpectedVersion: &version, Reason: "test grant"}
	updated, err := AdjustManagedUserQuota(input, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(math.MaxInt32)+1, updated.Quota)
	replay, err := AdjustManagedUserQuota(input, common.RoleRootUser)
	require.NoError(t, err)
	assert.Equal(t, updated.Quota, replay.Quota)
	input.EventID = "admin:operation-2"
	_, err = AdjustManagedUserQuota(input, common.RoleRootUser)
	assert.ErrorIs(t, err, ErrBillingVersionConflict)
}

func TestWalletIngressTokenBudgetUsesEventsAndVersion(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "token-owner", AffCode: "token-owner"}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Name: "budget", Key: "token-budget", RemainQuota: math.MaxInt32 + 1}
	require.NoError(t, token.Insert())
	assert.Equal(t, common.Quota(math.MaxInt32)+1, token.RemainQuota)
	version := token.QuotaVersion
	token.RemainQuota += 100
	require.NoError(t, token.UpdateWithBudget("raise-budget", &version))
	require.NoError(t, token.UpdateWithBudget("raise-budget", &version))
	token.Name = "changed-after-lost-response"
	assert.ErrorIs(t, token.UpdateWithBudget("raise-budget", &version), ErrBillingEventConflict)
	token.Name = "budget"
	token.RemainQuota += 1
	assert.ErrorIs(t, token.UpdateWithBudget("raise-budget", &version), ErrBillingEventConflict)
	var events []BillingEvent
	require.NoError(t, db.Where("token_id = ?", token.Id).Order("id").Find(&events).Error)
	require.Len(t, events, 2)
	assert.Equal(t, common.Quota(math.MaxInt32)+1, events[0].TokenAvailableDelta)
	assert.Equal(t, common.Quota(100), events[1].TokenAvailableDelta)
	token.UnlimitedQuota = true
	encoded, err := common.Marshal(token)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, common.Unmarshal(encoded, &body))
	assert.Nil(t, body["available_quota"])
	assert.Equal(t, "0", body["reserved_quota"])
}

func TestWalletIngressRedemptionHasAtomicEvent(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "redeemer", AffCode: "redeemer"}
	require.NoError(t, db.Create(&user).Error)
	redemption := Redemption{Key: "test-redemption", Name: "large", Quota: math.MaxInt32 + 1, Status: common.RedemptionCodeStatusEnabled}
	require.NoError(t, db.Create(&redemption).Error)
	amount, err := Redeem(redemption.Key, user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.Quota(math.MaxInt32)+1, amount)
	_, err = Redeem(redemption.Key, user.Id)
	require.Error(t, err)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, amount, user.Quota)
	var events int64
	require.NoError(t, db.Model(&BillingEvent{}).Where("source_type = ?", "redemption").Count(&events).Error)
	assert.Equal(t, int64(1), events)
}

func TestWalletIngressFailedPaymentCreditKeepsOrderPending(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "full-payer", AffCode: "full-payer", Quota: math.MaxInt64}
	require.NoError(t, db.Create(&user).Error)
	order := TopUp{UserId: user.Id, TradeNo: "payment-overflow", PaymentProvider: PaymentProviderEpay, Status: common.TopUpStatusPending, Amount: 1}
	payment := insertEpaySnapshotTestOrder(t, &order, 500_000, "test-key")
	_, err := RechargeEpay(payment, "")
	require.ErrorIs(t, err, ErrTopUpQuotaLimitExceeded)
	require.NoError(t, db.First(&order, order.Id).Error)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Zero(t, order.CreditedQuota)
	var events int64
	require.NoError(t, db.Model(&BillingEvent{}).Count(&events).Error)
	assert.Zero(t, events)
}

func TestWalletIngressZeroBudgetTokenCanReachPricing(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "free-model-user", AffCode: "free-model-user"}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "zero-budget-key", RemainQuota: 0, Status: common.TokenStatusEnabled, ExpiredTime: -1}
	require.NoError(t, token.Insert())
	authenticated, err := ValidateUserToken(token.Key)
	require.NoError(t, err)
	assert.Equal(t, token.Id, authenticated.Id)
	assert.Zero(t, authenticated.RemainQuota)
}

func TestWalletIngressDeletionProtectsZeroAmountRecovery(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "pending-free", AffCode: "pending-free"}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "pending-free-token"}
	require.NoError(t, token.Insert())
	require.NoError(t, db.Create(&BillingRecovery{RequestID: "pending-free", UserID: user.Id, TokenID: token.Id, Status: BillingRecoveryStatusActive, Source: "wallet"}).Error)
	require.Error(t, token.Delete())
	require.Error(t, user.Delete())
	require.NoError(t, db.First(&user, user.Id).Error)
	require.NoError(t, db.First(&token, token.Id).Error)
}

func TestTokenBudgetReplayIgnoresLaterPolicyAndReservation(t *testing.T) {
	db := useWalletIngressDB(t)
	user := User{Username: "replay-owner", AffCode: "replay-owner", Quota: 1000}
	require.NoError(t, db.Create(&user).Error)
	token := Token{UserId: user.Id, Key: "replay-key", Name: "replay", RemainQuota: 100}
	require.NoError(t, token.Insert())
	versionA := token.QuotaVersion
	token.UnlimitedQuota = true
	requestA := token
	require.NoError(t, token.UpdateWithBudget("policy-a", &versionA))
	versionB := token.QuotaVersion
	token.UnlimitedQuota = false
	require.NoError(t, token.UpdateWithBudget("policy-b", &versionB))
	_, err := EnsureBillingRecovery(BillingRecoveryInput{RequestID: "later-request", UserID: user.Id, TokenID: token.Id, TokenRequired: true, Source: "wallet"})
	require.NoError(t, err)
	require.NoError(t, Reserve("later-request", 10))
	require.NoError(t, requestA.UpdateWithBudget("policy-a", &versionA))
	assert.False(t, requestA.UnlimitedQuota)
	assert.EqualValues(t, 90, requestA.RemainQuota)
	assert.EqualValues(t, 10, requestA.ReservedQuota)
	var events int64
	require.NoError(t, db.Model(&BillingEvent{}).Where("source_id = ?", "policy-a").Count(&events).Error)
	assert.EqualValues(t, 1, events)
}
