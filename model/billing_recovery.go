package model

import (
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

var ErrBillingRecoveryStateConflict = errors.New("billing recovery state conflict")
var ErrBillingRecoveryTokenQuota = errors.New("billing recovery token quota insufficient")
var ErrBillingRecoveryFundingQuota = errors.New("billing recovery funding quota insufficient")

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
	BillingRecoveryOperationReserve = "reserve"
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
	TokenRequired     bool   `json:"token_required" gorm:"not null;default:false"`
	SettlementDelta   int64  `json:"settlement_delta" gorm:"not null;default:0"`
	PreConsumeState   string `json:"pre_consume_state" gorm:"type:varchar(16);index:billing_recovery_pending_scan,priority:2;not null;default:'prepared'"`
	FundingRefund     string `json:"funding_refund" gorm:"type:varchar(16);not null;default:'pending'"`
	ExtraRefund       string `json:"extra_refund" gorm:"type:varchar(16);not null;default:'pending'"`
	TokenRefund       string `json:"token_refund" gorm:"type:varchar(16);not null;default:'pending'"`
	FundingSettlement string `json:"funding_settlement" gorm:"type:varchar(16);not null;default:'pending'"`
	TokenSettlement   string `json:"token_settlement" gorm:"type:varchar(16);not null;default:'pending'"`
	Status            string `json:"status" gorm:"type:varchar(32);index;index:billing_recovery_pending_scan,priority:1;not null"`
	Attempts          int    `json:"attempts" gorm:"not null;default:0"`
	LastError         string `json:"last_error" gorm:"type:text"`
	CreatedAt         int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt         int64  `json:"updated_at" gorm:"bigint;index;index:billing_recovery_pending_scan,priority:3"`
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
	TokenRequired bool
}

func EnsureBillingRecovery(input BillingRecoveryInput) (*BillingRecovery, error) {
	if DB == nil || strings.TrimSpace(input.RequestID) == "" || input.UserID <= 0 || input.TokenID < 0 {
		return nil, errors.New("invalid billing recovery input")
	}
	if input.Source != "wallet" && input.Source != "subscription" {
		return nil, errors.New("invalid billing recovery funding source")
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
		return reconcileBillingRecoveryInput(&recovery, input)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	recovery = BillingRecovery{
		RequestID: input.RequestID, UserID: input.UserID, TokenID: input.TokenID,
		Source: input.Source, FundingAmount: input.FundingAmount, TokenAmount: input.TokenAmount, TokenRequired: input.TokenRequired,
	}
	if err := DB.Create(&recovery).Error; err != nil {
		if lookupErr := DB.Where("request_id = ?", input.RequestID).First(&recovery).Error; lookupErr == nil {
			return reconcileBillingRecoveryInput(&recovery, input)
		}
		return nil, err
	}
	return &recovery, nil
}

func reconcileBillingRecoveryInput(recovery *BillingRecovery, input BillingRecoveryInput) (*BillingRecovery, error) {
	if recovery == nil || recovery.UserID != input.UserID || recovery.TokenID != input.TokenID || recovery.TokenRequired != input.TokenRequired {
		return nil, errors.New("billing recovery request identity conflict")
	}
	if recovery.Source == input.Source {
		return recovery, nil
	}
	if !billingRecoveryIsReusable(*recovery) {
		return nil, errors.New("billing recovery request identity conflict")
	}

	var rebound BillingRecovery
	err := DB.Transaction(func(tx *gorm.DB) error {
		var current BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", input.RequestID).First(&current).Error; err != nil {
			return err
		}
		if current.UserID != input.UserID || current.TokenID != input.TokenID || current.TokenRequired != input.TokenRequired || !billingRecoveryIsReusable(current) {
			return errors.New("billing recovery request identity conflict")
		}
		if err := tx.Model(&current).Updates(map[string]any{
			"source":     input.Source,
			"status":     BillingRecoveryStatusActive,
			"last_error": "",
			"updated_at": common.GetTimestamp(),
		}).Error; err != nil {
			return err
		}
		current.Source = input.Source
		current.Status = BillingRecoveryStatusActive
		current.LastError = ""
		rebound = current
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &rebound, nil
}

func billingRecoveryIsReusable(recovery BillingRecovery) bool {
	return recovery.Status == BillingRecoveryStatusRefunded &&
		recovery.PreConsumeState == BillingRecoveryPreConsumePrepared &&
		recovery.FundingAmount == 0 && recovery.ExtraAmount == 0 && recovery.TokenAmount == 0
}

// AbortPreparedBillingRecovery closes an attempt which never reserved a
// balance. It is intentionally distinct from a refund: no completed amount is
// claimed and the same request may still use the configured fallback source.
func AbortPreparedBillingRecovery(requestID string, lastError error) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery abort")
	}
	message := ""
	if lastError != nil {
		message = lastError.Error()
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status == BillingRecoveryStatusRefunded {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusActive || recovery.PreConsumeState != BillingRecoveryPreConsumePrepared ||
			recovery.FundingAmount != 0 || recovery.ExtraAmount != 0 || recovery.TokenAmount != 0 {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
			"status":     BillingRecoveryStatusRefunded,
			"last_error": message,
			"updated_at": common.GetTimestamp(),
		}).Error
	})
}

func UpdateBillingRecoveryAmounts(requestID string, subscriptionID int, fundingAmount, extraAmount, tokenAmount int64) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery update")
	}
	if fundingAmount < 0 || extraAmount < 0 || tokenAmount < 0 {
		return errors.New("billing recovery amounts cannot be negative")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		// A late session sync must never rewrite the amounts that a completed
		// settlement or refund has already fenced with durable adjustments.
		if recovery.Status != BillingRecoveryStatusActive ||
			(recovery.PreConsumeState != BillingRecoveryPreConsumePrepared && recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted) {
			return ErrBillingRecoveryStateConflict
		}
		result := tx.Model(&BillingRecovery{}).
			Where("request_id = ? AND status = ?", requestID, BillingRecoveryStatusActive).
			Updates(map[string]any{
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
			return ErrBillingRecoveryStateConflict
		}
		return nil
	})
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
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery refund")
	}
	message := ""
	if lastError != nil {
		message = lastError.Error()
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status == BillingRecoveryStatusRefunded {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusActive && recovery.Status != BillingRecoveryStatusRefundPending {
			return ErrBillingRecoveryStateConflict
		}
		updates := map[string]any{
			"status":     BillingRecoveryStatusRefundPending,
			"last_error": message,
			"attempts":   gorm.Expr("attempts + 1"),
			"updated_at": common.GetTimestamp(),
		}
		// Subscription pre-consume has its own transaction. A process can
		// stop after it succeeds but before the session persists the amounts.
		// Only a still-consumed record proves that a refund is required.
		if recovery.Source == "subscription" && recovery.PreConsumeState == BillingRecoveryPreConsumePrepared {
			var record SubscriptionPreConsumeRecord
			err := tx.Where("request_id = ?", requestID).First(&record).Error
			if err == nil && record.Status == "consumed" {
				updates["subscription_id"] = record.UserSubscriptionId
				updates["funding_amount"] = record.PreConsumed
				if recovery.TokenRequired {
					updates["token_amount"] = record.PreConsumed
				}
				updates["pre_consume_state"] = BillingRecoveryPreConsumeCommitted
			} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(updates).Error
	})
}

func MarkBillingRecoverySettled(requestID string) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery settlement completion")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status == BillingRecoveryStatusSettled {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusSettlementPending || !BillingRecoverySettlementComplete(recovery) {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
			"status":     BillingRecoveryStatusSettled,
			"last_error": "",
			"updated_at": common.GetTimestamp(),
		}).Error
	})
}

func MarkBillingRecoveryRefunded(requestID string, lastError error) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery refund completion")
	}
	message := ""
	if lastError != nil {
		message = lastError.Error()
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status == BillingRecoveryStatusRefunded {
			return nil
		}
		if recovery.Status != BillingRecoveryStatusRefundPending || !BillingRecoveryRefundComplete(recovery) {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
			"status":     BillingRecoveryStatusRefunded,
			"last_error": message,
			"updated_at": common.GetTimestamp(),
		}).Error
	})
}

func ListStaleBillingRecoveries(cutoff int64, limit int) ([]BillingRecovery, error) {
	if DB == nil {
		return nil, errors.New("billing recovery database is unavailable")
	}
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	var rows []BillingRecovery
	err := DB.Where("status IN ? AND updated_at < ?", []string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending, BillingRecoveryStatusRefundPending}, cutoff).
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

// PreConsumeWalletBillingRecovery records wallet and token changes in the
// same transaction. TokenRequired is persisted with the request, preventing a
// later recovery from guessing whether a playground request charged a token.
func PreConsumeWalletBillingRecovery(requestID string, quota int64, tokenUnlimited bool) error {
	if DB == nil || strings.TrimSpace(requestID) == "" || quota < 0 {
		return errors.New("invalid billing recovery wallet pre-consume")
	}
	var mutation *billingRecoveryQuotaMutation
	if quota > 0 {
		var err error
		mutation, err = beginBillingRecoveryQuotaMutation(requestID, true, true)
		if err != nil {
			return err
		}
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		if mutation != nil {
			if err := mutation.applyPendingDeltas(tx); err != nil {
				return err
			}
		}
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Source != "wallet" || recovery.Status != BillingRecoveryStatusActive {
			return ErrBillingRecoveryStateConflict
		}
		if recovery.PreConsumeState == BillingRecoveryPreConsumeCommitted {
			if recovery.FundingAmount == quota && (!recovery.TokenRequired || recovery.TokenAmount == quota) {
				return nil
			}
			return ErrBillingRecoveryStateConflict
		}
		if recovery.TokenRequired && recovery.TokenID > 0 && quota > 0 {
			query := tx.Model(&Token{}).Where("id = ?", recovery.TokenID)
			if !tokenUnlimited {
				query = query.Where("remain_quota >= ?", quota)
			}
			result := query.Updates(map[string]any{
				"remain_quota":  gorm.Expr("remain_quota - ?", quota),
				"used_quota":    gorm.Expr("used_quota + ?", quota),
				"accessed_time": common.GetTimestamp(),
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingRecoveryTokenQuota
			}
		}
		if quota > 0 {
			result := tx.Model(&User{}).Where("id = ? AND quota >= ?", recovery.UserID, quota).Update("quota", gorm.Expr("quota - ?", quota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingRecoveryFundingQuota
			}
		}
		tokenAmount := int64(0)
		if recovery.TokenRequired {
			tokenAmount = quota
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
			"funding_amount":    quota,
			"token_amount":      tokenAmount,
			"pre_consume_state": BillingRecoveryPreConsumeCommitted,
			"updated_at":        common.GetTimestamp(),
		}).Error
	})
	if mutation != nil {
		mutation.release(err == nil)
	}
	if err != nil {
		return err
	}
	return nil
}

// ReserveBillingRecovery extends a committed reservation. The durable row is
// locked before calculating the delta, so stale in-memory sessions cannot
// repeat an already committed supplementary pre-consume.
func ReserveBillingRecovery(requestID string, targetQuota, delta int64, tokenUnlimited bool) (int64, error) {
	if DB == nil || strings.TrimSpace(requestID) == "" || targetQuota <= 0 || delta <= 0 {
		return 0, errors.New("invalid billing recovery reservation")
	}
	reservationComponent := fmt.Sprintf("reserve:%d", targetQuota)
	var appliedDelta int64
	mutation, err := beginBillingRecoveryQuotaMutation(requestID, true, true)
	if err != nil {
		return 0, err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := mutation.applyPendingDeltas(tx); err != nil {
			return err
		}
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status != BillingRecoveryStatusActive || recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted {
			return ErrBillingRecoveryStateConflict
		}
		reserved := recovery.FundingAmount
		if recovery.Source == "subscription" {
			reserved += recovery.ExtraAmount
		}
		expectedDelta := targetQuota - reserved
		if expectedDelta <= 0 {
			return nil
		}
		// The caller's delta is only a hint. Persisted amounts under the row
		// lock are authoritative after a restart or duplicate request.
		delta = expectedDelta
		var marker BillingRecoveryAdjustment
		markerErr := tx.Where("request_id = ? AND component = ? AND operation = ?", requestID, reservationComponent, BillingRecoveryOperationReserve).First(&marker).Error
		if markerErr == nil {
			if marker.Amount != expectedDelta {
				return ErrBillingRecoveryStateConflict
			}
			return nil
		}
		if !errors.Is(markerErr, gorm.ErrRecordNotFound) {
			return markerErr
		}

		updates := map[string]any{"updated_at": common.GetTimestamp()}
		if recovery.Source == "subscription" {
			if err := applySubscriptionDeltaTx(tx, recovery.SubscriptionID, delta); err != nil {
				return err
			}
			updates["extra_amount"] = gorm.Expr("extra_amount + ?", delta)
		} else {
			// Match the existing supplemental-wallet behavior: a later reserve
			// may carry a balance negative, while the initial pre-consume stays
			// guarded by the quota check above.
			result := tx.Model(&User{}).Where("id = ?", recovery.UserID).Update("quota", gorm.Expr("quota - ?", delta))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return gorm.ErrRecordNotFound
			}
			updates["funding_amount"] = gorm.Expr("funding_amount + ?", delta)
		}
		if recovery.TokenRequired && recovery.TokenID > 0 {
			query := tx.Model(&Token{}).Where("id = ?", recovery.TokenID)
			if !tokenUnlimited {
				query = query.Where("remain_quota >= ?", delta)
			}
			result := query.Updates(map[string]any{
				"remain_quota":  gorm.Expr("remain_quota - ?", delta),
				"used_quota":    gorm.Expr("used_quota + ?", delta),
				"accessed_time": common.GetTimestamp(),
			})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrBillingRecoveryTokenQuota
			}
			updates["token_amount"] = gorm.Expr("token_amount + ?", delta)
		}
		if err := tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(updates).Error; err != nil {
			return err
		}
		appliedDelta = delta
		return tx.Create(&BillingRecoveryAdjustment{
			RequestID: requestID,
			Component: reservationComponent,
			Operation: BillingRecoveryOperationReserve,
			Amount:    delta,
		}).Error
	})
	mutation.release(err == nil)
	if err != nil {
		return 0, err
	}
	return appliedDelta, nil
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
	mutation, err := beginBillingRecoveryQuotaMutation(
		requestID,
		component == BillingRecoveryComponentFunding,
		component == BillingRecoveryComponentToken,
	)
	if err != nil {
		return err
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := mutation.applyPendingDeltas(tx); err != nil {
			return err
		}
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
		if err := applyBillingRecoveryEffect(tx, recovery, component, operation, amount); err != nil {
			return err
		}
		if err := tx.Create(&BillingRecoveryAdjustment{RequestID: requestID, Component: component, Operation: operation, Amount: amount}).Error; err != nil {
			return err
		}
		return updateBillingRecoveryComponentState(tx, requestID, component, operation)
	})
	mutation.release(err == nil)
	if err != nil {
		return err
	}
	return nil
}

// billingRecoveryQuotaMutation closes the cache path for the selected
// resources and owns their pending batch deltas until the recovery transaction
// commits. This prevents the database fallback path from observing a balance
// that has already been reserved in Redis but not yet flushed to the database.
type billingRecoveryQuotaMutation struct {
	userID            int
	tokenID           int
	userPendingDelta  int
	tokenPendingDelta int
	batchLocked       bool
	unlockResources   func()
}

func beginBillingRecoveryQuotaMutation(requestID string, fenceUser, fenceToken bool) (*billingRecoveryQuotaMutation, error) {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return nil, errors.New("billing recovery database is unavailable")
	}
	var recovery BillingRecovery
	if err := DB.Select("user_id", "token_id", "token_required", "source").Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return nil, err
	}
	mutation := &billingRecoveryQuotaMutation{}
	if fenceUser && recovery.Source == "wallet" {
		mutation.userID = recovery.UserID
	}
	if fenceToken && recovery.TokenRequired && recovery.TokenID > 0 {
		mutation.tokenID = recovery.TokenID
	}
	mutation.unlockResources = lockQuotaMutationResources(mutation.userID, mutation.tokenID)
	if common.RedisEnabled && mutation.userID > 0 {
		if err := invalidateUserQuotaCacheForMutation(mutation.userID); err != nil {
			mutation.release(false)
			return nil, err
		}
	}
	if common.RedisEnabled && mutation.tokenID > 0 {
		var token Token
		if err := DB.Select("key").Where("id = ?", mutation.tokenID).First(&token).Error; err != nil {
			mutation.release(false)
			return nil, err
		}
		if err := invalidateTokenCacheForMutation(token.Key); err != nil {
			mutation.release(false)
			return nil, err
		}
	}
	// The Redis fence prevents new cache reservations before this process waits
	// for an in-flight batch flush. Holding the batch lock only for the drain
	// and durable transaction avoids turning a Redis outage into a global batch
	// updater stall.
	quotaBatchMutationLock.Lock()
	mutation.batchLocked = true
	mutation.userPendingDelta = takePendingQuotaDelta(BatchUpdateTypeUserQuota, mutation.userID)
	mutation.tokenPendingDelta = takePendingQuotaDelta(BatchUpdateTypeTokenQuota, mutation.tokenID)
	return mutation, nil
}

func takePendingQuotaDelta(type_, id int) int {
	if id <= 0 {
		return 0
	}
	batchUpdateLocks[type_].Lock()
	defer batchUpdateLocks[type_].Unlock()
	delta := batchUpdateStores[type_][id]
	delete(batchUpdateStores[type_], id)
	return delta
}

func (m *billingRecoveryQuotaMutation) applyPendingDeltas(tx *gorm.DB) error {
	if m == nil {
		return nil
	}
	if m.userPendingDelta != 0 {
		result := tx.Model(&User{}).Where("id = ?", m.userID).Update("quota", gorm.Expr("quota + ?", m.userPendingDelta))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	if m.tokenPendingDelta != 0 {
		result := tx.Model(&Token{}).Where("id = ?", m.tokenID).Updates(map[string]any{
			"remain_quota":  gorm.Expr("remain_quota + ?", m.tokenPendingDelta),
			"used_quota":    gorm.Expr("used_quota - ?", m.tokenPendingDelta),
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

func (m *billingRecoveryQuotaMutation) release(committed bool) {
	if m == nil {
		return
	}
	if !committed {
		if m.userPendingDelta != 0 {
			addNewRecord(BatchUpdateTypeUserQuota, m.userID, m.userPendingDelta)
		}
		if m.tokenPendingDelta != 0 {
			addNewRecord(BatchUpdateTypeTokenQuota, m.tokenID, m.tokenPendingDelta)
		}
	}
	if m.batchLocked {
		quotaBatchMutationLock.Unlock()
	}
	if m.unlockResources != nil {
		m.unlockResources()
	}
}

func applyBillingRecoveryEffect(tx *gorm.DB, recovery BillingRecovery, component, operation string, amount int64) error {
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
				err := refundBillingRecoverySubscriptionPreConsumeTx(tx, recovery.RequestID)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				return err
			}
			// Subscription pre-consume is already charged. Settlement applies
			// its signed delta directly, unlike the inverse wallet-balance edit.
			return applySubscriptionDeltaTx(tx, recovery.SubscriptionID, amount)
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

func refundBillingRecoverySubscriptionPreConsumeTx(tx *gorm.DB, requestID string) error {
	var record SubscriptionPreConsumeRecord
	if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&record).Error; err != nil {
		return err
	}
	if record.Status == "refunded" {
		return nil
	}
	if record.PreConsumed > 0 {
		if err := postConsumeUserSubscriptionDeltaTx(tx, record.UserSubscriptionId, -record.PreConsumed); err != nil {
			return err
		}
	}
	record.Status = "refunded"
	return tx.Save(&record).Error
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
