package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrBillingRecoveryStateConflict = errors.New("billing recovery state conflict")

const (
	BillingRecoveryStatusActive            = "active"
	BillingRecoveryStatusSettlementPending = "settlement_pending"
	BillingRecoveryStatusRefundPending     = "refund_pending"
	BillingRecoveryStatusSettled           = "settled"
	BillingRecoveryStatusRefunded          = "refunded"
	BillingRecoveryPreConsumePrepared      = "prepared"
	BillingRecoveryPreConsumeCommitted     = "committed"

	BillingRecoveryComponentFunding = "funding"
	BillingRecoveryComponentExtra   = "extra"
	BillingRecoveryComponentToken   = "token"
	BillingRecoveryOperationSettle  = "settle"
	BillingRecoveryOperationRefund  = "refund"
	BillingRecoveryStatePending     = "pending"
	BillingRecoveryStateCompleted   = "completed"
)

// BillingRecovery is the durable state for a request's pre-consume and
// settlement components. It contains identifiers and amounts only; token
// keys, headers, request bodies, and provider credentials are never stored.
type BillingRecovery struct {
	ID                int64  `json:"id" gorm:"primaryKey"`
	RequestID         string `json:"request_id" gorm:"type:varchar(128);uniqueIndex;not null"`
	UserID            int    `json:"user_id" gorm:"index;not null"`
	TokenID           int    `json:"token_id" gorm:"index;not null"`
	SubscriptionID    int    `json:"subscription_id" gorm:"index"`
	Source            string `json:"source" gorm:"type:varchar(32);not null"`
	FundingAmount     int64  `json:"funding_amount" gorm:"not null;default:0"`
	ExtraAmount       int64  `json:"extra_amount" gorm:"not null;default:0"`
	TokenAmount       int64  `json:"token_amount" gorm:"not null;default:0"`
	SettlementDelta   int64  `json:"settlement_delta" gorm:"not null;default:0"`
	PreConsumeState   string `json:"pre_consume_state" gorm:"type:varchar(16);not null;default:'prepared'"`
	FundingRefund     string `json:"funding_refund" gorm:"type:varchar(16);not null;default:'pending'"`
	ExtraRefund       string `json:"extra_refund" gorm:"type:varchar(16);not null;default:'pending'"`
	TokenRefund       string `json:"token_refund" gorm:"type:varchar(16);not null;default:'pending'"`
	FundingSettlement string `json:"funding_settlement" gorm:"type:varchar(16);not null;default:'pending'"`
	TokenSettlement   string `json:"token_settlement" gorm:"type:varchar(16);not null;default:'pending'"`
	Status            string `json:"status" gorm:"type:varchar(32);index;not null"`
	Attempts          int    `json:"attempts" gorm:"not null;default:0"`
	LastError         string `json:"last_error" gorm:"type:text"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;index"`
}

// BillingRecoveryAdjustment is the idempotency fence for one accounting
// effect. The unique key makes a retry after a process crash a no-op after
// the original database transaction committed.
type BillingRecoveryAdjustment struct {
	ID        int64  `json:"id" gorm:"primaryKey"`
	RequestID string `json:"request_id" gorm:"type:varchar(128);uniqueIndex:billing_recovery_adjustment_key;not null"`
	Component string `json:"component" gorm:"type:varchar(32);uniqueIndex:billing_recovery_adjustment_key;not null"`
	Operation string `json:"operation" gorm:"type:varchar(32);uniqueIndex:billing_recovery_adjustment_key;not null"`
	Amount    int64  `json:"amount" gorm:"not null;default:0"`
	CreatedAt int64  `json:"created_at" gorm:"bigint;index"`
}

func (r *BillingRecovery) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if r.Status == "" {
		r.Status = BillingRecoveryStatusActive
	}
	if r.PreConsumeState == "" {
		r.PreConsumeState = BillingRecoveryPreConsumePrepared
	}
	if r.FundingRefund == "" {
		r.FundingRefund = BillingRecoveryStatePending
	}
	if r.ExtraRefund == "" {
		r.ExtraRefund = BillingRecoveryStatePending
	}
	if r.TokenRefund == "" {
		r.TokenRefund = BillingRecoveryStatePending
	}
	if r.FundingSettlement == "" {
		r.FundingSettlement = BillingRecoveryStatePending
	}
	if r.TokenSettlement == "" {
		r.TokenSettlement = BillingRecoveryStatePending
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	return nil
}

func (r *BillingRecoveryAdjustment) BeforeCreate(_ *gorm.DB) error {
	if r.CreatedAt == 0 {
		r.CreatedAt = common.GetTimestamp()
	}
	return nil
}

type BillingRecoveryInput struct {
	RequestID     string
	UserID        int
	TokenID       int
	Source        string
	FundingAmount int64
	TokenAmount   int64
}

func EnsureBillingRecovery(input BillingRecoveryInput) (*BillingRecovery, error) {
	if DB == nil || strings.TrimSpace(input.RequestID) == "" || input.UserID <= 0 || input.TokenID < 0 {
		return nil, errors.New("invalid billing recovery input")
	}
	if input.FundingAmount < 0 || input.TokenAmount < 0 {
		return nil, errors.New("billing recovery amounts cannot be negative")
	}
	if input.TokenID == 0 && input.TokenAmount != 0 {
		return nil, errors.New("billing recovery token amount requires a token")
	}
	var recovery BillingRecovery
	err := DB.Where("request_id = ?", input.RequestID).First(&recovery).Error
	if err == nil {
		if recovery.UserID != input.UserID || recovery.TokenID != input.TokenID || recovery.Source != input.Source {
			return nil, errors.New("billing recovery request identity conflict")
		}
		return &recovery, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	recovery = BillingRecovery{
		RequestID: input.RequestID, UserID: input.UserID, TokenID: input.TokenID,
		Source: input.Source, FundingAmount: input.FundingAmount, TokenAmount: input.TokenAmount,
	}
	if err := DB.Create(&recovery).Error; err != nil {
		if lookupErr := DB.Where("request_id = ?", input.RequestID).First(&recovery).Error; lookupErr == nil {
			return &recovery, nil
		}
		return nil, err
	}
	return &recovery, nil
}

func UpdateBillingRecoveryAmounts(requestID string, subscriptionID int, fundingAmount, extraAmount, tokenAmount int64) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery update")
	}
	if fundingAmount < 0 || extraAmount < 0 || tokenAmount < 0 {
		return errors.New("billing recovery amounts cannot be negative")
	}
	result := DB.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
		"subscription_id":   subscriptionID,
		"funding_amount":    fundingAmount,
		"extra_amount":      extraAmount,
		"token_amount":      tokenAmount,
		"pre_consume_state": BillingRecoveryPreConsumeCommitted,
		"updated_at":        common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func UpdateBillingRecoverySettlement(requestID string, delta int64) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery settlement")
	}
	result := DB.Model(&BillingRecovery{}).Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending}).Updates(map[string]any{
		"settlement_delta": delta,
		"status":           BillingRecoveryStatusSettlementPending,
		"updated_at":       common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		var recovery BillingRecovery
		if err := DB.Select("status").Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		// A recovery worker may have completed the exact same settlement
		// between the caller's attempts. Do not overwrite its durable delta;
		// the adjustment markers remain the source of truth.
		if recovery.Status == BillingRecoveryStatusSettled {
			return nil
		}
		return ErrBillingRecoveryStateConflict
	}
	return nil
}

func TouchBillingRecovery(requestID string) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery heartbeat")
	}
	result := DB.Model(&BillingRecovery{}).
		Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending}).
		Update("updated_at", common.GetTimestamp())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func MarkBillingRecoveryRefundPending(requestID string, lastError error) error {
	message := ""
	if lastError != nil {
		message = lastError.Error()
	}
	result := DB.Model(&BillingRecovery{}).Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusActive, BillingRecoveryStatusRefundPending}).Updates(map[string]any{
		"status":     BillingRecoveryStatusRefundPending,
		"last_error": message,
		"attempts":   gorm.Expr("attempts + 1"),
		"updated_at": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var recovery BillingRecovery
	if err := DB.Select("status").Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	if recovery.Status == BillingRecoveryStatusRefunded {
		return nil
	}
	return ErrBillingRecoveryStateConflict
}

func MarkBillingRecoverySettled(requestID string) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery settlement completion")
	}
	result := DB.Model(&BillingRecovery{}).Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusSettlementPending, BillingRecoveryStatusSettled}).Updates(map[string]any{
		"status":     BillingRecoveryStatusSettled,
		"last_error": "",
		"updated_at": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var recovery BillingRecovery
	if err := DB.Select("status").Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	if recovery.Status == BillingRecoveryStatusSettled {
		return nil
	}
	return ErrBillingRecoveryStateConflict
}

func MarkBillingRecoveryRefunded(requestID string, lastError error) error {
	message := ""
	if lastError != nil {
		message = lastError.Error()
	}
	result := DB.Model(&BillingRecovery{}).Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusRefundPending, BillingRecoveryStatusRefunded}).Updates(map[string]any{
		"status":     BillingRecoveryStatusRefunded,
		"last_error": message,
		"updated_at": common.GetTimestamp(),
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	return ErrBillingRecoveryStateConflict
}

func ListStaleBillingRecoveries(cutoff int64, limit int) ([]BillingRecovery, error) {
	if DB == nil {
		return nil, errors.New("billing recovery database is unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var rows []BillingRecovery
	err := DB.Where("status IN ? AND pre_consume_state = ? AND updated_at < ?", []string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending, BillingRecoveryStatusRefundPending}, BillingRecoveryPreConsumeCommitted, cutoff).
		Order("updated_at asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func BillingRecoverySettlementComplete(recovery BillingRecovery) bool {
	return recovery.FundingSettlement == BillingRecoveryStateCompleted &&
		recovery.TokenSettlement == BillingRecoveryStateCompleted
}

func BillingRecoveryRefundComplete(recovery BillingRecovery) bool {
	if recovery.FundingRefund != BillingRecoveryStateCompleted {
		return false
	}
	if recovery.ExtraAmount > 0 && recovery.ExtraRefund != BillingRecoveryStateCompleted {
		return false
	}
	if recovery.TokenAmount > 0 && recovery.TokenRefund != BillingRecoveryStateCompleted {
		return false
	}
	return true
}

// ApplyBillingRecoveryAdjustment applies exactly one durable accounting effect.
// The adjustment marker and the balance update share one transaction.
func ApplyBillingRecoveryAdjustment(requestID, component, operation string) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("billing recovery database is unavailable")
	}
	if component != BillingRecoveryComponentFunding && component != BillingRecoveryComponentExtra && component != BillingRecoveryComponentToken {
		return errors.New("invalid billing recovery component")
	}
	if operation != BillingRecoveryOperationSettle && operation != BillingRecoveryOperationRefund {
		return errors.New("invalid billing recovery operation")
	}
	var userID, tokenID int
	var tokenKey string
	err := DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if operation == BillingRecoveryOperationRefund && recovery.Status == BillingRecoveryStatusSettled {
			return nil
		}
		var marker BillingRecoveryAdjustment
		markerErr := tx.Where("request_id = ? AND component = ? AND operation = ?", requestID, component, operation).First(&marker).Error
		if markerErr == nil {
			return nil
		}
		if !errors.Is(markerErr, gorm.ErrRecordNotFound) {
			return markerErr
		}
		switch operation {
		case BillingRecoveryOperationSettle:
			if component != BillingRecoveryComponentFunding && component != BillingRecoveryComponentToken {
				return errors.New("invalid billing recovery settlement component")
			}
			if recovery.Status != BillingRecoveryStatusSettlementPending {
				return ErrBillingRecoveryStateConflict
			}
		case BillingRecoveryOperationRefund:
			if recovery.Status == BillingRecoveryStatusSettled || recovery.Status == BillingRecoveryStatusRefunded {
				return nil
			}
			if recovery.Status != BillingRecoveryStatusActive && recovery.Status != BillingRecoveryStatusRefundPending {
				return ErrBillingRecoveryStateConflict
			}
		}

		amount := int64(0)
		switch component {
		case BillingRecoveryComponentFunding:
			amount = recovery.FundingAmount
		case BillingRecoveryComponentExtra:
			amount = recovery.ExtraAmount
		case BillingRecoveryComponentToken:
			amount = recovery.TokenAmount
		}
		if operation == BillingRecoveryOperationSettle && component == BillingRecoveryComponentFunding {
			amount = recovery.SettlementDelta
		}
		if operation == BillingRecoveryOperationSettle && component == BillingRecoveryComponentToken {
			amount = recovery.SettlementDelta
		}
		if err := applyBillingRecoveryEffect(tx, recovery, component, operation, amount, &tokenKey); err != nil {
			return err
		}
		if err := tx.Create(&BillingRecoveryAdjustment{RequestID: requestID, Component: component, Operation: operation, Amount: amount}).Error; err != nil {
			return err
		}
		userID, tokenID = recovery.UserID, recovery.TokenID
		return updateBillingRecoveryComponentState(tx, requestID, component, operation)
	})
	if err != nil {
		return err
	}
	if userID > 0 && component == BillingRecoveryComponentFunding && operation == BillingRecoveryOperationRefund {
		if cacheErr := invalidateUserCache(userID); cacheErr != nil {
			return cacheErr
		}
	}
	if tokenID > 0 && component == BillingRecoveryComponentToken && tokenKey != "" {
		if cacheErr := invalidateTokenCacheForMutation(tokenKey); cacheErr != nil {
			return cacheErr
		}
	}
	return nil
}

func applyBillingRecoveryEffect(tx *gorm.DB, recovery BillingRecovery, component, operation string, amount int64, tokenKey *string) error {
	if component == BillingRecoveryComponentToken && recovery.TokenID <= 0 {
		return nil
	}
	if amount == 0 {
		return nil
	}
	sign := int64(1)
	if operation == BillingRecoveryOperationSettle {
		sign = -1
	}
	switch component {
	case BillingRecoveryComponentFunding:
		if recovery.Source == "subscription" {
			if operation == BillingRecoveryOperationRefund {
				err := refundSubscriptionPreConsumeTx(tx, recovery.RequestID)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			return applySubscriptionDeltaTx(tx, recovery.SubscriptionID, amount*sign)
		}
		result := tx.Model(&User{}).Where("id = ?", recovery.UserID).Update("quota", gorm.Expr("quota + ?", amount*sign))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	case BillingRecoveryComponentExtra:
		return applySubscriptionDeltaTx(tx, recovery.SubscriptionID, -amount)
	case BillingRecoveryComponentToken:
		var token Token
		if err := tx.Select("id", "key").Where("id = ?", recovery.TokenID).First(&token).Error; err != nil {
			return err
		}
		*tokenKey = token.Key
		result := tx.Model(&Token{}).Where("id = ?", recovery.TokenID).Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota + ?", amount*sign),
			"used_quota":    gorm.Expr("used_quota - ?", amount*sign),
			"accessed_time": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func updateBillingRecoveryComponentState(tx *gorm.DB, requestID, component, operation string) error {
	column := ""
	if operation == BillingRecoveryOperationRefund {
		switch component {
		case BillingRecoveryComponentFunding:
			column = "funding_refund"
		case BillingRecoveryComponentExtra:
			column = "extra_refund"
		case BillingRecoveryComponentToken:
			column = "token_refund"
		}
	} else {
		switch component {
		case BillingRecoveryComponentFunding:
			column = "funding_settlement"
		case BillingRecoveryComponentToken:
			column = "token_settlement"
		}
	}
	if column == "" {
		return nil
	}
	return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
		column:       BillingRecoveryStateCompleted,
		"updated_at": common.GetTimestamp(),
	}).Error
}

func applySubscriptionDeltaTx(tx *gorm.DB, subscriptionID int, delta int64) error {
	if subscriptionID <= 0 || delta == 0 {
		return nil
	}
	return postConsumeUserSubscriptionDeltaTx(tx, subscriptionID, delta)
}

func postConsumeUserSubscriptionDeltaTx(tx *gorm.DB, subscriptionID int, delta int64) error {
	var sub UserSubscription
	if err := lockForUpdate(tx).Where("id = ?", subscriptionID).First(&sub).Error; err != nil {
		return err
	}
	newUsed := sub.AmountUsed + delta
	if newUsed < 0 {
		newUsed = 0
	}
	if sub.AmountTotal > 0 && newUsed > sub.AmountTotal {
		return fmt.Errorf("subscription used exceeds total, used=%d total=%d", newUsed, sub.AmountTotal)
	}
	sub.AmountUsed = newUsed
	return tx.Save(&sub).Error
}
