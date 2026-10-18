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

var (
	ErrAffiliateDisabled          = errors.New("referral program is disabled; remove the invitation code to register")
	ErrAffiliateCodeInvalid       = errors.New("invitation code is invalid or disabled")
	ErrAffiliateOperation         = errors.New("a valid operation_id is required")
	ErrAffiliateOperationConflict = errors.New("operation_id was already used for a different request")
	ErrAffiliateUnavailable       = errors.New("no available referral quota")
)
var affiliateCodePattern = regexp.MustCompile(`^[A-Z0-9_-]{4,32}$`)
var affiliateOperationPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,96}$`)

// AffiliateRecord is the dedicated referral history, not a general wallet ledger.
// SourceKey is unique across a payment, an admin addition or a user's transfer.
type AffiliateRecord struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id" gorm:"index;not null"`
	InviteeID      int    `json:"invitee_id" gorm:"index"`
	Kind           string `json:"kind" gorm:"type:varchar(16);index"`
	SourceType     string `json:"source_type" gorm:"type:varchar(24)"`
	SourceID       string `json:"source_id" gorm:"type:varchar(128)"`
	SourceKey      string `json:"-" gorm:"type:varchar(180);uniqueIndex;not null"`
	BaseQuota      int    `json:"base_quota" gorm:"type:bigint"`
	RateBps        int    `json:"rate_bps"`
	RewardQuota    int    `json:"reward_quota" gorm:"type:bigint"`
	SkipReason     string `json:"skip_reason" gorm:"type:varchar(40)"`
	FrozenUntil    int64  `json:"frozen_until" gorm:"index"`
	ThawedAt       int64  `json:"thawed_at"`
	CreatedAt      int64  `json:"created_at" gorm:"index"`
	AvailableAfter int    `json:"available_after" gorm:"type:bigint"`
	FrozenAfter    int    `json:"frozen_after" gorm:"type:bigint"`
	WalletAfter    int    `json:"wallet_after" gorm:"type:bigint"`
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
		total += r.RewardQuota
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
	record := AffiliateRecord{UserID: user.InviterId, InviteeID: user.Id, Kind: "accrue", SourceType: sourceType, SourceID: sourceID, SourceKey: key, BaseQuota: baseQuota, RateBps: rate, CreatedAt: now, AvailableAfter: inviter.AffQuota, FrozenAfter: inviter.AffFrozenQuota}
	switch {
	case inviterUnavailable:
		// Deleting an inviter must not invalidate an invitee's paid credit.
		// Persist the decision so restoring that user cannot backfill a reward.
		record.SkipReason = "inviter_unavailable"
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
	if inviter.AffHistoryQuota > common.MaxWalletQuota-reward || inviter.AffQuota > common.MaxWalletQuota-reward || inviter.AffFrozenQuota > common.MaxWalletQuota-reward {
		return ErrWalletQuotaLimitExceeded
	}
	record.RewardQuota = reward
	if policy.FreezeHours > 0 {
		record.FrozenUntil = now + int64(policy.FreezeHours)*3600
		inviter.AffFrozenQuota += reward
	} else {
		record.ThawedAt = now
		inviter.AffQuota += reward
	}
	inviter.AffHistoryQuota += reward
	record.AvailableAfter, record.FrozenAfter = inviter.AffQuota, inviter.AffFrozenQuota
	if err := tx.Create(&record).Error; err != nil {
		return err
	}
	return tx.Model(&User{}).Where("id = ?", inviter.Id).Updates(map[string]any{"aff_quota": inviter.AffQuota, "aff_frozen_quota": inviter.AffFrozenQuota, "aff_history": inviter.AffHistoryQuota}).Error
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
			replay = true
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
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
		return tx.Create(&result).Error
	})
	if err == nil && !replay {
		syncCreditUserQuotaCache(userID, result.RewardQuota, "affiliate transfer")
	}
	return &result, replay, err
}
