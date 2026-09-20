package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrBillingRecoveryStateConflict = errors.New("billing recovery state conflict")
var ErrBillingRecoveryTokenQuota = errors.New("billing recovery token quota insufficient")
var ErrBillingRecoveryFundingQuota = errors.New("billing recovery funding quota insufficient")
var ErrUnsupportedBillingSource = errors.New("billing source is retired; manual reconciliation is required")

const (
	BillingRecoveryStatusActive            = "active"
	BillingRecoveryStatusSettlementPending = "settlement_pending"
	BillingRecoveryStatusRefundPending     = "refund_pending"
	BillingRecoveryStatusSettled           = "settled"
	BillingRecoveryStatusRefunded          = "refunded"
	BillingRecoveryPreConsumePrepared      = "prepared"
	BillingRecoveryPreConsumeCommitted     = "committed"
	BillingRecoveryOperationSettle         = "settle"
	BillingRecoveryOperationRefund         = "refund"
	BillingRecoveryOperationReserve        = "reserve"
	BillingRecoveryStatePending            = "pending"
	BillingRecoveryStateCompleted          = "completed"
	BillingRecoveryStateUnprepared         = "unprepared"
	BillingRecoveryStateWriting            = "writing"
	BillingRecoveryStateCollecting         = "collecting"
)

// BillingRecovery is the durable state for a request's pre-consume and
// settlement components and the sanitized consumption log snapshot. Token
// keys, headers, request bodies, and provider credentials are never stored.
type BillingRecovery struct {
	Version        int64  `json:"version" gorm:"type:bigint;not null;default:0"`
	QuoteSnapshot  string `json:"-" gorm:"type:text"`
	UsageSnapshot  string `json:"-" gorm:"type:text"`
	ChargeSnapshot string `json:"-" gorm:"type:text"`
	FinalEventID   string `json:"event_id" gorm:"type:varchar(192)"`

	ID                    int64        `json:"id" gorm:"primaryKey"`
	RequestID             string       `json:"request_id" gorm:"type:varchar(128);uniqueIndex;not null"`
	UserID                int          `json:"user_id" gorm:"index;not null"`
	TokenID               int          `json:"token_id" gorm:"index;not null"`
	Source                string       `json:"source" gorm:"type:varchar(32);not null"`
	FundingAmount         common.Quota `json:"funding_amount" gorm:"type:bigint;not null;default:0"`
	TokenAmount           common.Quota `json:"token_amount" gorm:"type:bigint;not null;default:0"`
	TokenRequired         bool         `json:"token_required" gorm:"not null;default:false"`
	SettlementDelta       common.Quota `json:"settlement_delta" gorm:"type:bigint;not null;default:0"`
	PreConsumeState       string       `json:"pre_consume_state" gorm:"type:varchar(16);index:billing_recovery_pending_scan,priority:2;not null;default:'prepared'"`
	Status                string       `json:"status" gorm:"type:varchar(32);index;index:billing_recovery_pending_scan,priority:1;not null"`
	Attempts              int          `json:"attempts" gorm:"not null;default:0"`
	LastError             string       `json:"last_error" gorm:"type:text"`
	ConsumptionSnapshot   string       `json:"-" gorm:"type:text"`
	ConsumptionState      string       `json:"consumption_state" gorm:"type:varchar(16);index"`
	ConsumptionStatistics string       `json:"consumption_statistics" gorm:"type:varchar(16)"`
	ConsumptionLog        string       `json:"consumption_log" gorm:"type:varchar(16)"`
	ConsumptionError      string       `json:"consumption_error" gorm:"type:text"`
	CreatedAt             int64        `json:"created_at" gorm:"bigint;index"`
	UpdatedAt             int64        `json:"updated_at" gorm:"bigint;index;index:billing_recovery_pending_scan,priority:3"`
}

func (r *BillingRecovery) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if r.Status == "" {
		r.Status = BillingRecoveryStatusActive
	}
	if r.PreConsumeState == "" {
		r.PreConsumeState = BillingRecoveryPreConsumePrepared
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	return nil
}

type BillingRecoveryInput struct {
	RequestID     string
	UserID        int
	TokenID       int
	Source        string
	FundingAmount common.Quota
	TokenAmount   common.Quota
	TokenRequired bool
}

func EnsureBillingRecovery(input BillingRecoveryInput) (*BillingRecovery, error) {
	var recovery *BillingRecovery
	err := BillingTransaction(context.Background(), func(tx *gorm.DB) error {
		var err error
		recovery, err = ensureBillingRecoveryTx(tx, input)
		return err
	})
	return recovery, err
}

func ensureBillingRecoveryTx(tx *gorm.DB, input BillingRecoveryInput) (*BillingRecovery, error) {
	if tx == nil || strings.TrimSpace(input.RequestID) == "" || len(input.RequestID) > 128 || input.UserID <= 0 || input.TokenID < 0 || input.TokenRequired && input.TokenID == 0 {
		return nil, errors.New("invalid billing recovery input")
	}
	if input.Source != "wallet" {
		return nil, ErrUnsupportedBillingSource
	}
	if input.FundingAmount != 0 || input.TokenAmount != 0 {
		return nil, errors.New("new recoveries must not claim an existing reservation")
	}
	if input.TokenID == 0 && input.TokenAmount != 0 {
		return nil, errors.New("billing recovery token amount requires a token")
	}
	recovery := BillingRecovery{
		RequestID: input.RequestID, UserID: input.UserID, TokenID: input.TokenID,
		Source: input.Source, FundingAmount: input.FundingAmount, TokenAmount: input.TokenAmount, TokenRequired: input.TokenRequired,
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "request_id"}}, DoNothing: true}).Create(&recovery).Error; err != nil {
		return nil, err
	}
	var existing BillingRecovery
	if err := lockForUpdate(tx).Where("request_id = ?", input.RequestID).First(&existing).Error; err != nil {
		return nil, err
	}
	return reconcileBillingRecoveryInput(&existing, input)
}

func reconcileBillingRecoveryInput(recovery *BillingRecovery, input BillingRecoveryInput) (*BillingRecovery, error) {
	if recovery == nil || recovery.UserID != input.UserID || recovery.TokenID != input.TokenID || recovery.TokenRequired != input.TokenRequired || recovery.Source != input.Source {
		return nil, errors.New("billing recovery request identity conflict")
	}
	return recovery, nil
}

// AbortPreparedBillingRecovery closes an attempt which never reserved a
// balance. It is intentionally distinct from a refund: no completed amount is
// claimed for a request which never reserved funds.
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
			recovery.FundingAmount != 0 || recovery.TokenAmount != 0 {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(map[string]any{
			"status":                 BillingRecoveryStatusRefunded,
			"version":                recovery.Version + 1,
			"consumption_state":      BillingRecoveryStateCompleted,
			"consumption_statistics": BillingRecoveryStateCompleted,
			"consumption_log":        BillingRecoveryStateCompleted,
			"last_error":             message,
			"updated_at":             common.GetTimestamp(),
		}).Error
	})
}

func UpdateBillingRecoverySettlement(requestID string, delta int64) error {
	if DB == nil || strings.TrimSpace(requestID) == "" {
		return errors.New("invalid billing recovery settlement")
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		var recovery BillingRecovery
		if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
			return err
		}
		if recovery.Status == BillingRecoveryStatusSettled {
			if recovery.SettlementDelta != common.Quota(delta) {
				return ErrBillingEventConflict
			}
			return nil
		}
		if recovery.Status == BillingRecoveryStatusSettlementPending {
			if recovery.SettlementDelta != common.Quota(delta) {
				return ErrBillingRecoveryStateConflict
			}
			return nil
		}
		if recovery.Status != BillingRecoveryStatusActive {
			return ErrBillingRecoveryStateConflict
		}
		actual, err := common.AddQuota(recovery.FundingAmount, common.Quota(delta))
		if err != nil || actual < 0 || actual > common.MaxQuota {
			return ErrBillingRecoveryStateConflict
		}
		return tx.Model(&recovery).Updates(map[string]any{
			"version":          recovery.Version + 1,
			"settlement_delta": delta,
			"status":           BillingRecoveryStatusSettlementPending,
			"updated_at":       common.GetTimestamp(),
		}).Error
	})
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
		// MySQL reports zero changed rows when a heartbeat repeats within the
		// same second. Existence in a renewable state is the relevant condition.
		var count int64
		if err := DB.Model(&BillingRecovery{}).Where("request_id = ? AND status IN ?", requestID, []string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending}).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return gorm.ErrRecordNotFound
		}
	}
	return nil
}

func MarkBillingRecoveryRefundPending(requestID string, lastError error) error {
	return markBillingRecoveryRefundPending(requestID, lastError, false)
}

func MarkBillingRecoveryAutomaticRefundPending(requestID string) error {
	return markBillingRecoveryRefundPending(requestID, nil, true)
}

func markBillingRecoveryRefundPending(requestID string, lastError error, automatic bool) error {
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
		if BillingRecoveryUsageCollecting(recovery.ConsumptionState) || recovery.ConsumptionState == BillingRecoveryStateTaskPending {
			return ErrBillingRecoveryStateConflict
		}
		if automatic && recovery.Status == BillingRecoveryStatusActive && BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
			return ErrBillingRecoveryStateConflict
		}
		updates := map[string]any{
			"status":     BillingRecoveryStatusRefundPending,
			"last_error": message,
			"attempts":   gorm.Expr("attempts + 1"),
			"updated_at": common.GetTimestamp(),
		}
		if recovery.Source != "wallet" {
			return ErrUnsupportedBillingSource
		}
		return tx.Model(&BillingRecovery{}).Where("request_id = ?", requestID).Updates(updates).Error
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
	err := staleBillingRecoveries(DB, cutoff).
		Where("consumption_state IS NULL OR consumption_state <> ?", BillingRecoveryStateTaskPending).
		Where("status <> ?", BillingRecoveryStatusReconciliation).
		Where("status NOT IN ? OR consumption_state IS NULL OR consumption_state NOT IN ?",
			[]string{BillingRecoveryStatusActive, BillingRecoveryStatusSettled}, billingRecoveryUnconfirmedStates()).
		Order("updated_at asc").Limit(limit).Find(&rows).Error
	return rows, err
}

func staleBillingRecoveries(db *gorm.DB, cutoff int64) *gorm.DB {
	return db.Model(&BillingRecovery{}).Where("(status IN ? OR (status IN ? AND consumption_state IN ?)) AND updated_at < ?",
		[]string{BillingRecoveryStatusActive, BillingRecoveryStatusSettlementPending, BillingRecoveryStatusRefundPending, BillingRecoveryStatusReconciliation},
		[]string{BillingRecoveryStatusSettled, BillingRecoveryStatusRefunded}, append(billingRecoveryUnconfirmedStates(), BillingRecoveryStatePending), cutoff)
}

type BillingRecoveryBacklog struct {
	Pending       int64 `json:"pending"`
	OldestUpdated int64 `json:"oldest_updated_at"`
}

func ReadBillingRecoveryBacklog(ctx context.Context, cutoff int64) (BillingRecoveryBacklog, error) {
	var backlog BillingRecoveryBacklog
	if DB == nil {
		return backlog, errors.New("billing recovery database is unavailable")
	}
	err := staleBillingRecoveries(DB.WithContext(ctx), cutoff).
		Select("COUNT(*) AS pending, COALESCE(MIN(updated_at), 0) AS oldest_updated").Scan(&backlog).Error
	return backlog, err
}

// Reserve and IncreaseReservation always reserve the cumulative target. A
// cheaper retry keeps the existing hold; a more expensive retry must fund it.
func Reserve(requestID string, target common.Quota) error {
	return BillingTransaction(context.Background(), func(tx *gorm.DB) error { return ReserveTx(tx, requestID, target) })
}

func IncreaseReservation(requestID string, target common.Quota) error {
	return Reserve(requestID, target)
}

func ReserveTx(tx *gorm.DB, requestID string, target common.Quota) error {
	if requestID == "" || target < 0 || target > math.MaxInt32 {
		return errors.New("invalid reservation amount")
	}
	var recovery BillingRecovery
	if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	if recovery.Source != "wallet" {
		return ErrUnsupportedBillingSource
	}
	if recovery.Status != BillingRecoveryStatusActive {
		return ErrBillingRecoveryStateConflict
	}
	if recovery.PreConsumeState == BillingRecoveryPreConsumeCommitted && target <= recovery.FundingAmount {
		return nil
	}
	delta := target - recovery.FundingAmount
	if recovery.PreConsumeState == BillingRecoveryPreConsumePrepared && recovery.FundingAmount != 0 {
		return ErrBillingRecoveryStateConflict
	}
	event := &BillingEvent{EventID: fmt.Sprintf("%s:reserve:%d", requestID, target), RequestID: requestID, UserID: recovery.UserID, Operation: "reserve", SourceType: "request", SourceID: requestID, AvailableDelta: -delta, ReservedDelta: delta}
	if recovery.TokenRequired {
		event.TokenID = recovery.TokenID
		event.TokenAvailableDelta = -delta
		event.TokenReservedDelta = delta
	}
	fingerprint, err := billingEventFingerprint(struct {
		RequestID string
		Target    common.Quota
	}{requestID, target})
	if err != nil {
		return err
	}
	event.Fingerprint = fingerprint
	// Zero holds for explicit free requests still have a durable event, and
	// therefore the same dispatch and reconciliation protections as paid calls.
	if _, err := applyAccountEventTx(tx, event, nil, target > 0); err != nil {
		return err
	}
	tokenAmount := common.Quota(0)
	if recovery.TokenRequired {
		tokenAmount = target
	}
	return tx.Model(&recovery).Updates(map[string]any{"funding_amount": target, "token_amount": tokenAmount, "pre_consume_state": BillingRecoveryPreConsumeCommitted, "version": recovery.Version + 1, "updated_at": common.GetTimestamp()}).Error
}

// Settle applies actual consumption, including debt beyond the estimate. The
// reservation, both account projections, event and recovery state commit once.
func Settle(requestID string, actual common.Quota) error {
	return BillingTransaction(context.Background(), func(tx *gorm.DB) error { return SettleTx(tx, requestID, actual) })
}

func Release(requestID string) error {
	return BillingTransaction(context.Background(), func(tx *gorm.DB) error { return ReleaseTx(tx, requestID) })
}

// SettleTx and ReleaseTx let business documents share the accounting commit.
func SettleTx(tx *gorm.DB, requestID string, actual common.Quota) error {
	return finalizeBillingTx(tx, requestID, actual, false, "", 0, "")
}
func ReleaseTx(tx *gorm.DB, requestID string) error {
	return finalizeBillingTx(tx, requestID, 0, true, "", 0, "")
}

func finalizeBillingTx(tx *gorm.DB, requestID string, actual common.Quota, release bool, operationID string, actor int, reason string, expectedVersion ...int64) error {
	if requestID == "" || actual < 0 || actual > math.MaxInt32 {
		return errors.New("invalid settlement amount")
	}
	var recovery BillingRecovery
	if err := lockForUpdate(tx).Where("request_id = ?", requestID).First(&recovery).Error; err != nil {
		return err
	}
	if recovery.Source != "wallet" {
		return ErrUnsupportedBillingSource
	}
	operation, status := "settle", BillingRecoveryStatusSettled
	if release {
		operation, status = "release", BillingRecoveryStatusRefunded
	}
	if operationID == "" {
		operationID = requestID + ":" + operation
	}
	fingerprint, err := billingEventFingerprint(struct {
		RequestID, Operation string
		Quota                common.Quota
		Actor                int
		Reason               string
		ExpectedVersion      []int64
	}{requestID, operation, actual, actor, reason, expectedVersion})
	if err != nil {
		return err
	}
	if event, err := findBillingEvent(tx, operationID, fingerprint); err != nil || event != nil {
		return err
	}
	if recovery.Status == BillingRecoveryStatusSettled || recovery.Status == BillingRecoveryStatusRefunded {
		return ErrBillingEventConflict
	}
	if recovery.PreConsumeState != BillingRecoveryPreConsumeCommitted {
		return ErrBillingRecoveryStateConflict
	}
	if actor == 0 {
		if release && recovery.Status != BillingRecoveryStatusRefundPending {
			return ErrBillingRecoveryStateConflict
		}
		if !release && recovery.Status != BillingRecoveryStatusSettlementPending {
			return ErrBillingRecoveryStateConflict
		}
	}
	reserved := recovery.FundingAmount
	var consumption BillingConsumption
	if recovery.ConsumptionSnapshot != "" && !BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
		if err := common.UnmarshalJsonStr(recovery.ConsumptionSnapshot, &consumption); err != nil {
			return err
		}
		if consumption.Log.Quota != actual {
			return ErrBillingEventConflict
		}
	} else {
		consumption = BillingConsumption{Log: Log{RequestId: requestID, UserId: recovery.UserID, TokenId: recovery.TokenID, Quota: actual, Type: LogTypeConsume, CreatedAt: common.GetTimestamp()}, LogEnabled: !release && common.LogConsumeEnabled}
		if release {
			consumption.Log.Type = LogTypeRefund
		}
	}
	consumption.Log.BillingEventID = &operationID
	outbox, err := common.Marshal(consumption)
	if err != nil {
		return err
	}
	event := &BillingEvent{EventID: operationID, RequestID: requestID, UserID: recovery.UserID, Operation: operation, SourceType: "request", SourceID: requestID, Fingerprint: fingerprint, AvailableDelta: reserved - actual, ReservedDelta: -reserved, UsedDelta: actual, ActorID: actor, Reason: reason}
	if recovery.TokenRequired {
		event.TokenID = recovery.TokenID
		event.TokenAvailableDelta = recovery.TokenAmount - actual
		event.TokenReservedDelta = -recovery.TokenAmount
		event.TokenUsedDelta = actual
	}
	if _, err := applyAccountEventTx(tx, event, nil, false); err != nil {
		return err
	}
	updates := map[string]any{"status": status, "settlement_delta": actual - reserved, "version": recovery.Version + 1, "last_error": "", "updated_at": common.GetTimestamp(), "final_event_id": operationID, "consumption_snapshot": string(outbox), "consumption_state": BillingRecoveryStatePending}
	if recovery.ConsumptionSnapshot == "" || BillingRecoveryUsageUnconfirmed(recovery.ConsumptionState) {
		updates["consumption_statistics"], updates["consumption_log"] = BillingRecoveryStatePending, BillingRecoveryStatePending
	}
	if !consumption.CountUsage && !consumption.LogEnabled && !consumption.ExportEnabled && consumption.TaskAdjustment == nil {
		updates["consumption_state"], updates["consumption_statistics"], updates["consumption_log"] = BillingRecoveryStateCompleted, BillingRecoveryStateCompleted, BillingRecoveryStateCompleted
	}
	return tx.Model(&recovery).Updates(updates).Error
}
