package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrBillingEventConflict = errors.New("billing event identity conflict")
var ErrBillingVersionConflict = errors.New("billing account version conflict")
var billingIdempotencyConflicts atomic.Int64

// Automatic settlement and manual reconciliation share one event identity.
type billingFinalizationIdentity struct {
	RequestID, Operation string
	Quota                common.Quota
	Actor                int
	Reason               string
	ExpectedVersion      []int64
	EvidenceHash         string
}

// BillingEvent is both the idempotency fence and the permanent accounting
// evidence. It is never updated when a display log is retried or removed.
type BillingEvent struct {
	ID                  int64        `json:"id" gorm:"primaryKey"`
	EventID             string       `json:"event_id" gorm:"type:varchar(192);uniqueIndex;not null"`
	RequestID           string       `json:"request_id" gorm:"type:varchar(128);index"`
	UserID              int          `json:"user_id" gorm:"index;not null"`
	TokenID             int          `json:"token_id" gorm:"index"`
	Operation           string       `json:"operation" gorm:"type:varchar(32);not null"`
	SourceType          string       `json:"source_type" gorm:"type:varchar(32)"`
	SourceID            string       `json:"source_id" gorm:"type:varchar(192)"`
	Fingerprint         string       `json:"-" gorm:"type:varchar(64);not null"`
	EvidenceHash        string       `json:"evidence_hash,omitempty" gorm:"type:varchar(64)"`
	AvailableDelta      common.Quota `json:"available_delta" gorm:"type:bigint;not null"`
	ReservedDelta       common.Quota `json:"reserved_delta" gorm:"type:bigint;not null"`
	UsedDelta           common.Quota `json:"used_delta" gorm:"type:bigint;not null"`
	TokenAvailableDelta common.Quota `json:"token_available_delta" gorm:"type:bigint;not null"`
	TokenReservedDelta  common.Quota `json:"token_reserved_delta" gorm:"type:bigint;not null"`
	TokenUsedDelta      common.Quota `json:"token_used_delta" gorm:"type:bigint;not null"`
	AvailableAfter      common.Quota `json:"available_after" gorm:"type:bigint;not null"`
	ReservedAfter       common.Quota `json:"reserved_after" gorm:"type:bigint;not null"`
	UsedAfter           common.Quota `json:"used_after" gorm:"type:bigint;not null"`
	TokenAvailableAfter common.Quota `json:"token_available_after" gorm:"type:bigint;not null"`
	TokenReservedAfter  common.Quota `json:"token_reserved_after" gorm:"type:bigint;not null"`
	TokenUsedAfter      common.Quota `json:"token_used_after" gorm:"type:bigint;not null"`
	ActorID             int          `json:"actor_id"`
	Reason              string       `json:"reason" gorm:"type:varchar(512)"`
	CreatedAt           int64        `json:"created_at" gorm:"bigint;index"`
}

func (*BillingEvent) BeforeUpdate(*gorm.DB) error { return errors.New("billing events are immutable") }
func (*BillingEvent) BeforeDelete(*gorm.DB) error { return errors.New("billing events are immutable") }

type WalletAdjustment struct {
	EventID         string
	UserID          int
	Delta           common.Quota
	ActorID         int
	Reason          string
	SourceType      string
	SourceID        string
	ExpectedVersion *int64
	TargetBalance   *common.Quota
}

func ApplyWalletAdjustment(input WalletAdjustment) (*BillingEvent, error) {
	var event *BillingEvent
	err := BillingTransaction(context.Background(), func(tx *gorm.DB) error {
		var err error
		event, err = ApplyWalletAdjustmentTx(tx, input)
		return err
	})
	return event, err
}

// ApplyWalletAdjustmentTx joins the business document transaction. A retried
// absolute adjustment checks its event before the account version, so a lost
// response cannot turn a successful operation into a conflict.
func ApplyWalletAdjustmentTx(tx *gorm.DB, input WalletAdjustment) (*BillingEvent, error) {
	if tx == nil || input.UserID <= 0 || strings.TrimSpace(input.EventID) == "" || len(input.EventID) > 192 || len(input.Reason) > 512 {
		return nil, errors.New("invalid wallet adjustment")
	}
	fingerprint, err := billingEventFingerprint(input)
	if err != nil {
		return nil, err
	}
	var user User
	if err := lockForUpdate(tx).Where("id = ?", input.UserID).First(&user).Error; err != nil {
		return nil, err
	}
	if existing, err := findBillingEvent(tx, input.EventID, fingerprint); err != nil || existing != nil {
		return existing, err
	}
	if input.ExpectedVersion != nil && user.QuotaVersion != *input.ExpectedVersion {
		return nil, ErrBillingVersionConflict
	}
	delta := input.Delta
	if input.TargetBalance != nil {
		if input.ExpectedVersion == nil {
			return nil, errors.New("balance assignment requires expected version")
		}
		delta, err = common.SubtractQuota(*input.TargetBalance, user.Quota)
		if err != nil {
			return nil, err
		}
	}
	event := &BillingEvent{EventID: input.EventID, UserID: input.UserID, Operation: "adjustment", SourceType: input.SourceType, SourceID: input.SourceID, AvailableDelta: delta, ActorID: input.ActorID, Reason: input.Reason, Fingerprint: fingerprint}
	if delta >= 0 && input.TargetBalance == nil {
		event.Operation = "credit"
	}
	return applyAccountEventTx(tx, event, &user, false)
}

// applyAccountEventTx is the sole writer of wallet and token accounting
// projections. Callers lock request/business state first, then wallet, then
// token. Redis and display log delivery happen outside this transaction.
func applyAccountEventTx(tx *gorm.DB, event *BillingEvent, user *User, requireAvailable bool) (*BillingEvent, error) {
	if user == nil {
		user = &User{}
		if err := lockForUpdate(tx).Select("id", "available_quota", "reserved_quota", "used_quota", "quota_version").Where("id = ?", event.UserID).First(user).Error; err != nil {
			return nil, err
		}
	}
	if existing, err := findBillingEvent(tx, event.EventID, event.Fingerprint); err != nil || existing != nil {
		return existing, err
	}
	available, err := common.AddQuota(user.Quota, event.AvailableDelta)
	if err != nil {
		return nil, err
	}
	reserved, err := common.AddQuota(user.ReservedQuota, event.ReservedDelta)
	if err != nil {
		return nil, err
	}
	used, err := common.AddQuota(user.UsedQuota, event.UsedDelta)
	if err != nil {
		return nil, err
	}
	if reserved < 0 || used < 0 {
		return nil, ErrBillingRecoveryStateConflict
	}
	if requireAvailable && available < 0 {
		return nil, ErrBillingRecoveryFundingQuota
	}
	if event.TokenID > 0 {
		var token Token
		if err := lockForUpdate(tx).Select("id", "available_quota", "reserved_quota", "used_quota", "quota_version", "unlimited_quota").Where("id = ? AND user_id = ?", event.TokenID, event.UserID).First(&token).Error; err != nil {
			return nil, err
		}
		if token.UnlimitedQuota {
			event.TokenAvailableDelta = 0
		}
		ta, err := common.AddQuota(token.RemainQuota, event.TokenAvailableDelta)
		if err != nil {
			return nil, err
		}
		tr, err := common.AddQuota(token.ReservedQuota, event.TokenReservedDelta)
		if err != nil {
			return nil, err
		}
		tu, err := common.AddQuota(token.UsedQuota, event.TokenUsedDelta)
		if err != nil {
			return nil, err
		}
		if tr < 0 || tu < 0 {
			return nil, ErrBillingRecoveryStateConflict
		}
		if requireAvailable && !token.UnlimitedQuota && ta < 0 {
			return nil, ErrBillingRecoveryTokenQuota
		}
		if token.QuotaVersion == int64(^uint64(0)>>1) {
			return nil, common.ErrQuotaOverflow
		}
		if err := tx.Model(&token).Updates(map[string]any{"available_quota": ta, "reserved_quota": tr, "used_quota": tu, "quota_version": token.QuotaVersion + 1, "accessed_time": common.GetTimestamp()}).Error; err != nil {
			return nil, err
		}
		event.TokenAvailableAfter, event.TokenReservedAfter, event.TokenUsedAfter = ta, tr, tu
	}
	if user.QuotaVersion == int64(^uint64(0)>>1) {
		return nil, common.ErrQuotaOverflow
	}
	if err := tx.Model(user).Updates(map[string]any{"available_quota": available, "reserved_quota": reserved, "used_quota": used, "quota_version": user.QuotaVersion + 1}).Error; err != nil {
		return nil, err
	}
	event.AvailableAfter, event.ReservedAfter, event.UsedAfter = available, reserved, used
	event.CreatedAt = common.GetTimestamp()
	if err := tx.Create(event).Error; err != nil {
		return nil, err
	}
	return event, nil
}

// BillingTransaction retries only transactions the database definitively
// rejected before commit. Ambiguous commit errors must be retried by event ID.
func BillingTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	if DB == nil {
		return errors.New("billing database is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for attempt := 0; ; attempt++ {
		err := DB.WithContext(ctx).Transaction(fn)
		if err == nil || attempt == 3 || !isBillingLockConflict(err) {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func isBillingLockConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") || strings.Contains(message, "database table is locked") || strings.Contains(message, "deadlock detected") || strings.Contains(message, "deadlock found") || strings.Contains(message, "could not serialize access")
}

func billingEventFingerprint(value any) (string, error) {
	encoded, err := common.Marshal(value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded)), nil
}

func findBillingEvent(tx *gorm.DB, id, fingerprint string) (*BillingEvent, error) {
	var existing BillingEvent
	err := tx.Where("event_id = ?", id).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if existing.Fingerprint != fingerprint {
		billingIdempotencyConflicts.Add(1)
		return nil, ErrBillingEventConflict
	}
	return &existing, nil
}
