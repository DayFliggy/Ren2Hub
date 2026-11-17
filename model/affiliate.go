package model

import (
	"crypto/rand"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// These sentinels reach the console, so they carry a message_key; errors.Is
// still matches them by identity.
var (
	ErrAffiliateDisabled          = common.NewMessage("referral program is disabled; remove the invitation code to register")
	ErrAffiliateCodeInvalid       = common.NewMessage("invitation code is invalid or disabled")
	ErrAffiliateOperation         = common.NewMessage("a valid operation_id is required")
	ErrAffiliateOperationConflict = common.NewMessage("operation_id was already used for a different request")
	ErrAffiliateUnavailable       = common.NewMessage("no available referral quota")
	ErrAffiliateCompliance        = common.NewMessage("invitation rewards are disabled until payment compliance is confirmed")
)
var affiliateCodePattern = regexp.MustCompile(`^[A-Z0-9_-]{4,32}$`)
var affiliateOperationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// AffiliateRecord is the dedicated referral history, not a general wallet ledger.
// SourceKey is unique across a payment, an admin addition or a user's transfer.
type AffiliateRecord struct {
	ID              int    `json:"id"`
	UserID          int    `json:"user_id" gorm:"index;not null"`
	InviteeID       int    `json:"invitee_id" gorm:"index"`
	Kind            string `json:"kind" gorm:"type:varchar(24);index"`
	SourceType      string `json:"source_type" gorm:"type:varchar(24)"`
	SourceID        string `json:"source_id" gorm:"type:varchar(128)"`
	SourceKey       string `json:"-" gorm:"type:varchar(180);uniqueIndex;not null"`
	RelatedRecordID int    `json:"-" gorm:"index;not null;default:0"`
	Reversible      bool   `json:"-" gorm:"index;not null;default:false"`
	BaseQuota       int    `json:"base_quota" gorm:"type:bigint"`
	RateBps         int    `json:"rate_bps"`
	RewardQuota     int    `json:"reward_quota" gorm:"type:bigint"`
	CreditedQuota   int    `json:"credited_quota" gorm:"type:bigint;not null;default:0"`
	DebtOffsetQuota int    `json:"debt_offset_quota" gorm:"type:bigint;not null;default:0"`
	DebtAddedQuota  int    `json:"debt_added_quota" gorm:"type:bigint;not null;default:0"`
	SkipReason      string `json:"skip_reason" gorm:"type:varchar(40)"`
	FrozenUntil     int64  `json:"frozen_until" gorm:"index"`
	ThawedAt        int64  `json:"thawed_at"`
	CreatedAt       int64  `json:"created_at" gorm:"index"`
	AvailableAfter  int    `json:"available_after" gorm:"type:bigint"`
	FrozenAfter     int    `json:"frozen_after" gorm:"type:bigint"`
	WalletAfter     int    `json:"wallet_after" gorm:"type:bigint"`
}

func creditedAffiliateQuota(record AffiliateRecord) int {
	// Existing rows predate credited_quota and were credited in full.
	if record.CreditedQuota == 0 && record.DebtOffsetQuota == 0 && record.RewardQuota > 0 && record.Kind == "accrue" {
		return record.RewardQuota
	}
	return record.CreditedQuota
}

func sumAffiliateRelatedQuota(tx *gorm.DB, relatedID int, kind, column string) (int, error) {
	// Locking reads observe rows committed while this transaction waited for the
	// wallet lock, including under MySQL's default REPEATABLE READ isolation.
	var values []int64
	if err := lockForUpdate(tx).Model(&AffiliateRecord{}).Where("related_record_id = ? AND kind = ?", relatedID, kind).Pluck(column, &values).Error; err != nil {
		return 0, err
	}
	var total int64
	for _, value := range values {
		if value < 0 || value > int64(common.MaxWalletQuota)-total {
			return 0, ErrWalletQuotaLimitExceeded
		}
		total += value
	}
	return int(total), nil
}

// EnsureAffiliateCode fills a missing legacy invitation code under the user
// row lock. The unique index remains the final guard against concurrent writers.
func EnsureAffiliateCode(userID int) (string, error) {
	var code string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var user User
		if err := lockForUpdate(tx).First(&user, userID).Error; err != nil {
			return err
		}
		if user.AffCode != "" {
			code = user.AffCode
			return nil
		}
		for attempt := 0; attempt < 8; attempt++ {
			candidate, err := newAffiliateCode()
			if err != nil {
				return err
			}
			var count int64
			if err = tx.Unscoped().Model(&User{}).Where("aff_code = ?", candidate).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				continue
			}
			result := tx.Model(&User{}).Where("id = ? AND (aff_code IS NULL OR aff_code = ?)", userID, "").Update("aff_code", candidate)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			code = candidate
			return nil
		}
		return errors.New("could not allocate invitation code")
	})
	return code, err
}

func newAffiliateCode() (string, error) {
	const alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ_"
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// InitializeAffiliateUserTx runs before Create in the registration transaction.
// A supplied invalid code fails the whole registration, never silently losing attribution.
func InitializeAffiliateUserTx(tx *gorm.DB, user *User, rawCode string) error {
	user.InviterId, user.InvitedAt = 0, 0
	user.AffCodeEnabled = true
	user.AffCodeCustom = false
	var err error
	for attempt := 0; attempt < 8; attempt++ {
		user.AffCode, err = newAffiliateCode()
		if err != nil {
			return err
		}
		var count int64
		if err = tx.Unscoped().Model(&User{}).Where("aff_code = ?", user.AffCode).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			break
		}
		if attempt == 7 {
			return errors.New("could not allocate invitation code")
		}
	}
	code := strings.ToUpper(strings.TrimSpace(rawCode))
	if code == "" {
		return nil
	}
	if !operation_setting.SnapshotAffiliateSetting().Enabled {
		return ErrAffiliateDisabled
	}
	if !affiliateCodePattern.MatchString(code) {
		return ErrAffiliateCodeInvalid
	}
	var inviter User
	if err = lockForUpdate(tx).Where("aff_code = ?", code).First(&inviter).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrAffiliateCodeInvalid
		}
		return err
	}
	if !inviter.AffCodeEnabled || inviter.Status != common.UserStatusEnabled {
		return ErrAffiliateCodeInvalid
	}
	user.InviterId, user.InvitedAt = inviter.Id, common.GetTimestamp()
	return tx.Model(&User{}).Where("id = ?", inviter.Id).UpdateColumn("aff_count", gorm.Expr("aff_count + 1")).Error
}

// lockAffiliateParticipantsTx orders wallet locks consistently across top-ups,
// admin additions and transfers, including when two invitees share an inviter.
func lockAffiliateParticipantsTx(tx *gorm.DB, userID int) (*User, error) {
	var reference User
	if err := tx.Select("id", "inviter_id").First(&reference, userID).Error; err != nil {
		return nil, err
	}
	ids := []int{userID}
	if reference.InviterId > 0 && reference.InviterId != userID {
		ids = append(ids, reference.InviterId)
	}
	var rows []User
	if err := lockForUpdate(tx).Where("id IN ?", ids).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].Id == userID {
			return &rows[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func thawAffiliateTx(tx *gorm.DB, user *User, now int64) error {
	var records []AffiliateRecord
	if err := lockForUpdate(tx).Where("user_id = ? AND kind = ? AND frozen_until > 0 AND frozen_until <= ? AND thawed_at = 0", user.Id, "accrue", now).Find(&records).Error; err != nil {
		return err
	}
	total := 0
	ids := make([]int, 0, len(records))
	for _, r := range records {
		reversed, err := sumAffiliateRelatedQuota(tx, r.ID, "reversal", "credited_quota")
		if err != nil {
			return err
		}
		remaining := creditedAffiliateQuota(r) - reversed
		if remaining < 0 {
			return errors.New("affiliate reversal exceeds frozen reward")
		}
		total += remaining
		ids = append(ids, r.ID)
	}
	if total == 0 {
		return nil
	}
	if total > user.AffFrozenQuota || user.AffQuota > common.MaxWalletQuota-total {
		return ErrWalletQuotaLimitExceeded
	}
	user.AffFrozenQuota -= total
	user.AffQuota += total
	if err := tx.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{"aff_quota": user.AffQuota, "aff_frozen_quota": user.AffFrozenQuota}).Error; err != nil {
		return err
	}
	return tx.Model(&AffiliateRecord{}).Where("id IN ? AND thawed_at = 0", ids).Update("thawed_at", now).Error
}

func applyAffiliateRewardTx(tx *gorm.DB, user *User, sourceType, sourceID string, baseQuota int) error {
	// No referral relationship means no recipient-owned referral history. Every
	// source for an existing relationship records its decision, including zero.
	if user.InviterId <= 0 {
		return nil
	}
	if user.InviterId == user.Id {
		return errors.New("invalid self referral")
	}
	var inviter User
	err := lockForUpdate(tx).First(&inviter, user.InviterId).Error
	inviterUnavailable := errors.Is(err, gorm.ErrRecordNotFound)
	if err != nil && !inviterUnavailable {
		return err
	}
	key := sourceType + ":" + sourceID
	var existing AffiliateRecord
	err = lockForUpdate(tx).Where("source_key = ?", key).First(&existing).Error
	if err == nil {
		if existing.UserID != user.InviterId || existing.InviteeID != user.Id || existing.BaseQuota != baseQuota {
			return errors.New("affiliate source conflicts with the recorded decision")
		}
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	policy := operation_setting.SnapshotAffiliateSetting()
	now := common.GetTimestamp()
	rate := policy.RateBps
	if inviter.AffRebateRateBps != nil {
		rate = *inviter.AffRebateRateBps
	}
	if rate < 0 || rate > 10000 {
		return errors.New("invalid affiliate rate")
	}
	record := AffiliateRecord{UserID: user.InviterId, InviteeID: user.Id, Kind: "accrue", SourceType: sourceType, SourceID: sourceID, SourceKey: key, BaseQuota: baseQuota, RateBps: rate, CreatedAt: now, AvailableAfter: inviter.AffQuota, FrozenAfter: inviter.AffFrozenQuota, Reversible: sourceType == "admin_quota"}
	switch {
	case inviterUnavailable:
		// Deleting an inviter must not invalidate an invitee's paid credit.
		// Persist the decision so restoring that user cannot backfill a reward.
		record.SkipReason = "inviter_unavailable"
	case !operation_setting.IsPaymentComplianceConfirmed():
		record.SkipReason = "compliance_unconfirmed"
	case !policy.Enabled:
		record.SkipReason = "program_disabled"
	case sourceType == "admin_quota" && !policy.AdminRechargeEnabled:
		record.SkipReason = "admin_recharge_disabled"
	case baseQuota <= 0:
		record.SkipReason = "no_paid_quota"
	case policy.DurationDays > 0 && (user.InvitedAt <= 0 || now >= user.InvitedAt+int64(policy.DurationDays)*86400):
		record.SkipReason = "referral_expired"
	case inviter.Status != common.UserStatusEnabled:
		record.SkipReason = "inviter_disabled"
	case rate == 0:
		record.SkipReason = "zero_rate"
	}
	if record.SkipReason != "" {
		return tx.Create(&record).Error
	}
	reward := int(decimal.NewFromInt(int64(baseQuota)).Mul(decimal.NewFromInt(int64(rate))).Div(decimal.NewFromInt(10000)).Floor().IntPart())
	if reward == 0 {
		record.SkipReason = "below_quota_unit"
		return tx.Create(&record).Error
	}
	if policy.PerInviteeCapQuota > 0 {
		// Current reads avoid an old snapshot after waiting for the inviter lock
		// on MySQL REPEATABLE READ.
		var previous []AffiliateRecord
		if err := lockForUpdate(tx).Select("id", "reward_quota").Where("user_id = ? AND invitee_id = ? AND kind = ?", inviter.Id, user.Id, "accrue").Find(&previous).Error; err != nil {
			return err
		}
		total := 0
		for _, previousRecord := range previous {
			total += previousRecord.RewardQuota
		}
		remaining := policy.PerInviteeCapQuota - total
		if remaining <= 0 {
			record.SkipReason = "invitee_cap_reached"
			return tx.Create(&record).Error
		}
		if reward > remaining {
			reward = remaining
		}
	}
	if inviter.AffHistoryQuota > common.MaxWalletQuota-reward {
		return ErrWalletQuotaLimitExceeded
	}
	offset := reward
	if offset > inviter.AffOffsetDebtQuota {
		offset = inviter.AffOffsetDebtQuota
	}
	credited := reward - offset
	if inviter.AffQuota > common.MaxWalletQuota-credited || inviter.AffFrozenQuota > common.MaxWalletQuota-credited {
		return ErrWalletQuotaLimitExceeded
	}
	record.RewardQuota = reward
	record.CreditedQuota = credited
	record.DebtOffsetQuota = offset
	inviter.AffOffsetDebtQuota -= offset
	if policy.FreezeHours > 0 {
		record.FrozenUntil = now + int64(policy.FreezeHours)*3600
		inviter.AffFrozenQuota += credited
	} else {
		record.ThawedAt = now
		inviter.AffQuota += credited
	}
	inviter.AffHistoryQuota += reward
	record.AvailableAfter, record.FrozenAfter = inviter.AffQuota, inviter.AffFrozenQuota
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	return tx.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": inviter.AffQuota, "aff_frozen_quota": inviter.AffFrozenQuota, "aff_history": inviter.AffHistoryQuota, "aff_offset_debt_quota": inviter.AffOffsetDebtQuota}).Error
}

// reverseAdminAffiliateRewardsTx reverses only post-migration tracked admin
// rebates. Older records lack transfer-level attribution and are deliberately
// left untouched so this migration cannot alter previously transferable quota.
func reverseAdminAffiliateRewardsTx(tx *gorm.DB, invitee *User, sourceID string, reduceBase int) error {
	if reduceBase <= 0 || invitee.InviterId <= 0 {
		return nil
	}
	var inviter User
	if err := lockForUpdate(tx).Unscoped().First(&inviter, invitee.InviterId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	var accruals []AffiliateRecord
	if err := lockForUpdate(tx).Where("user_id = ? AND invitee_id = ? AND kind = ? AND reversible = ? AND reward_quota > 0", inviter.Id, invitee.Id, "accrue", true).Order("id ASC").Find(&accruals).Error; err != nil {
		return err
	}
	now := common.GetTimestamp()
	for _, accrual := range accruals {
		if reduceBase <= 0 {
			break
		}
		usedBase, err := sumAffiliateRelatedQuota(tx, accrual.ID, "reversal", "base_quota")
		if err != nil {
			return err
		}
		usedReward, err := sumAffiliateRelatedQuota(tx, accrual.ID, "reversal", "reward_quota")
		if err != nil {
			return err
		}
		usedCredited, err := sumAffiliateRelatedQuota(tx, accrual.ID, "reversal", "credited_quota")
		if err != nil {
			return err
		}
		usedDebt, err := sumAffiliateRelatedQuota(tx, accrual.ID, "reversal", "debt_added_quota")
		if err != nil {
			return err
		}
		usedOffset, err := sumAffiliateRelatedQuota(tx, accrual.ID, "reversal", "debt_offset_quota")
		if err != nil {
			return err
		}
		baseLeft := accrual.BaseQuota - usedBase
		if baseLeft <= 0 {
			continue
		}
		baseDelta := reduceBase
		if baseDelta > baseLeft {
			baseDelta = baseLeft
		}
		newBase := usedBase + baseDelta
		grossTarget := int(decimal.NewFromInt(int64(accrual.RewardQuota)).Mul(decimal.NewFromInt(int64(newBase))).Div(decimal.NewFromInt(int64(accrual.BaseQuota))).Floor().IntPart())
		if newBase == accrual.BaseQuota {
			grossTarget = accrual.RewardQuota
		}
		// Revoke the portion used to repay debt first. Both components then grow
		// monotonically as principal is corrected, including sub-quota rounding.
		offsetTarget := min(grossTarget, accrual.DebtOffsetQuota)
		creditedTarget := grossTarget - offsetTarget
		usedTransferredDebt := usedDebt - usedOffset
		if usedTransferredDebt < 0 {
			return errors.New("invalid affiliate reversal debt ledger")
		}
		usedRevokedCredit := usedCredited + usedTransferredDebt
		grossDelta := grossTarget - usedReward
		offsetDelta := offsetTarget - usedOffset
		creditedDelta := creditedTarget - usedRevokedCredit
		if grossDelta < 0 || offsetDelta < 0 || creditedDelta < 0 || offsetDelta+creditedDelta != grossDelta {
			return errors.New("invalid affiliate reversal ledger state")
		}
		transferred, err := sumAffiliateRelatedQuota(tx, accrual.ID, "transfer_allocation", "reward_quota")
		if err != nil {
			return err
		}
		availableCredit := creditedAffiliateQuota(accrual) - usedCredited - transferred
		if availableCredit < 0 {
			return errors.New("affiliate transfer allocation exceeds credited reward")
		}
		remainingTransferred := transferred - usedTransferredDebt
		if remainingTransferred < 0 {
			return errors.New("affiliate reversal exceeds transferred reward")
		}
		removeCredit := creditedDelta
		if removeCredit > availableCredit {
			removeCredit = availableCredit
		}
		transferredDebt := creditedDelta - removeCredit
		if transferredDebt > remainingTransferred {
			return errors.New("affiliate reversal exceeds attributable reward")
		}
		if accrual.FrozenUntil > 0 && accrual.ThawedAt == 0 {
			if removeCredit > inviter.AffFrozenQuota {
				return errors.New("affiliate frozen balance is inconsistent with ledger")
			}
			inviter.AffFrozenQuota -= removeCredit
		} else {
			if removeCredit > inviter.AffQuota {
				return errors.New("affiliate available balance is inconsistent with ledger")
			}
			inviter.AffQuota -= removeCredit
		}
		debtDelta := offsetDelta + transferredDebt
		if debtDelta > common.MaxWalletQuota-inviter.AffOffsetDebtQuota {
			return ErrWalletQuotaLimitExceeded
		}
		inviter.AffOffsetDebtQuota += debtDelta
		key := fmt.Sprintf("admin_quota_reversal:%s:%d", sourceID, accrual.ID)
		entry := AffiliateRecord{UserID: inviter.Id, InviteeID: invitee.Id, Kind: "reversal", SourceType: "admin_quota_reversal", SourceID: sourceID, SourceKey: key, RelatedRecordID: accrual.ID, BaseQuota: baseDelta, RateBps: accrual.RateBps, RewardQuota: grossDelta, CreditedQuota: removeCredit, DebtOffsetQuota: offsetDelta, DebtAddedQuota: debtDelta, CreatedAt: now, AvailableAfter: inviter.AffQuota, FrozenAfter: inviter.AffFrozenQuota}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		reduceBase -= baseDelta
	}
	return tx.Unscoped().Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": inviter.AffQuota, "aff_frozen_quota": inviter.AffFrozenQuota, "aff_offset_debt_quota": inviter.AffOffsetDebtQuota}).Error
}

// TransferAffiliateQuota moves all currently available referral quota exactly once.
func TransferAffiliateQuota(userID int, operationID string) (*AffiliateRecord, bool, error) {
	if !affiliateOperationPattern.MatchString(operationID) {
		return nil, false, ErrAffiliateOperation
	}
	key := fmt.Sprintf("transfer:%d:%s", userID, operationID)
	var result AffiliateRecord
	replay := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		user, err := lockAffiliateParticipantsTx(tx, userID)
		if err != nil {
			return err
		}
		err = lockForUpdate(tx).Where("source_key = ?", key).First(&result).Error
		if err == nil {
			if result.UserID != userID || result.Kind != "transfer" || result.SourceType != "transfer" || result.SourceID != operationID {
				return ErrAffiliateOperationConflict
			}
			// A committed transfer remains replayable after policy or balance
			// changes; replay returns its receipt without moving quota again.
			replay = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !operation_setting.IsPaymentComplianceConfirmed() {
			return ErrAffiliateCompliance
		}
		now := common.GetTimestamp()
		if err = thawAffiliateTx(tx, user, now); err != nil {
			return err
		}
		amount := user.AffQuota
		if amount <= 0 {
			return ErrAffiliateUnavailable
		}
		if err = creditTopUpQuota(tx, userID, amount, nil); err != nil {
			return err
		}
		if err = tx.Model(&User{}).Where("id = ?", userID).Update("aff_quota", 0).Error; err != nil {
			return err
		}
		result = AffiliateRecord{UserID: userID, Kind: "transfer", SourceType: "transfer", SourceID: operationID, SourceKey: key, RewardQuota: amount, FrozenAfter: user.AffFrozenQuota, WalletAfter: user.Quota + amount, CreatedAt: now}
		if err = tx.Create(&result).Error; err != nil {
			return err
		}
		return appendAffiliateTransferAllocationsTx(tx, user, result.ID, operationID, amount)
	})
	if err == nil && !replay {
		syncCreditUserQuotaCache(userID, result.RewardQuota, "affiliate transfer")
	}
	return &result, replay, err
}

// affiliateTrackedAccrual is the available part of one thawed reversible
// accrual that is still attributed to it.
type affiliateTrackedAccrual struct {
	record AffiliateRecord
	amount int
}

// trackedAffiliateAvailableTx returns the available referral quota still
// attributed to thawed reversible (administrator-addition) accruals, one entry
// per accrual, and its total. The rest of user.AffQuota (top-up rewards and
// legacy balances) is fungible and not attributed to any accrual.
func trackedAffiliateAvailableTx(tx *gorm.DB, user *User) ([]affiliateTrackedAccrual, int, error) {
	var accruals []AffiliateRecord
	if err := lockForUpdate(tx).Where("user_id = ? AND kind = ? AND reversible = ?", user.Id, "accrue", true).Order("id ASC").Find(&accruals).Error; err != nil {
		return nil, 0, err
	}
	tracked := make([]affiliateTrackedAccrual, 0, len(accruals))
	total := 0
	for _, accrual := range accruals {
		if accrual.FrozenUntil > 0 && accrual.ThawedAt == 0 {
			continue
		}
		credited := creditedAffiliateQuota(accrual)
		reversed, err := sumAffiliateRelatedQuota(tx, accrual.ID, "reversal", "credited_quota")
		if err != nil {
			return nil, 0, err
		}
		transferred, err := sumAffiliateRelatedQuota(tx, accrual.ID, "transfer_allocation", "reward_quota")
		if err != nil {
			return nil, 0, err
		}
		remaining := credited - reversed - transferred
		if remaining < 0 {
			return nil, 0, errors.New("affiliate accrual balance is inconsistent with ledger")
		}
		if remaining == 0 {
			continue
		}
		total += remaining
		tracked = append(tracked, affiliateTrackedAccrual{record: accrual, amount: remaining})
	}
	if total > user.AffQuota {
		return nil, 0, errors.New("affiliate available balance is below its ledger allocations")
	}
	return tracked, total, nil
}

func appendAffiliateTransferAllocationsTx(tx *gorm.DB, user *User, transferID int, operationID string, amount int) error {
	allocations, trackedAvailable, err := trackedAffiliateAvailableTx(tx, user)
	if err != nil {
		return err
	}
	remainingTransfer := amount
	// Legacy balances and non-admin/unattributed rewards are fungible. Consume
	// them first so new admin rewards remain attributable until necessary.
	legacyAvailable := user.AffQuota - trackedAvailable
	if remainingTransfer > legacyAvailable {
		remainingTransfer -= legacyAvailable
	} else {
		remainingTransfer = 0
	}
	for _, item := range allocations {
		if remainingTransfer == 0 {
			break
		}
		allocated := item.amount
		if allocated > remainingTransfer {
			allocated = remainingTransfer
		}
		entry := AffiliateRecord{UserID: user.Id, InviteeID: item.record.InviteeID, Kind: "transfer_allocation", SourceType: "transfer_allocation", SourceID: operationID, SourceKey: fmt.Sprintf("transfer_allocation:%d:%d", transferID, item.record.ID), RelatedRecordID: item.record.ID, RewardQuota: allocated, CreatedAt: common.GetTimestamp()}
		if err := tx.Create(&entry).Error; err != nil {
			return err
		}
		remainingTransfer -= allocated
	}
	if remainingTransfer != 0 {
		return errors.New("affiliate transfer amount exceeds available ledger balance")
	}
	return nil
}
