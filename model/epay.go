package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

// Credentials are private records, never part of an order API response.
type EpayMerchantVersion struct {
	ID          string `json:"-" gorm:"primaryKey;size:36"`
	Fingerprint string `json:"-" gorm:"uniqueIndex;size:64"`
	AccountID   string `json:"-" gorm:"size:64;not null"`
	GatewayURL  string `json:"-" gorm:"type:text;not null"`
	MerchantID  string `json:"-" gorm:"size:128;not null"`
	Key         string `json:"-" gorm:"type:text;not null"`
}

// Quote fields are immutable. Only receipt and query scheduling fields change.
type EpayOrder struct {
	TopUpID           int          `json:"-" gorm:"primaryKey;autoIncrement:false"`
	MerchantVersionID string       `json:"-" gorm:"size:36;not null"`
	AccountID         string       `json:"-" gorm:"size:64;not null;uniqueIndex:idx_epay_receipt,priority:1"`
	GatewayTradeNo    *string      `json:"gateway_trade_no,omitempty" gorm:"size:128;uniqueIndex:idx_epay_receipt,priority:2"`
	InputAmount       int64        `json:"input_amount,string" gorm:"type:bigint;not null"`
	AmountUnit        string       `json:"amount_unit" gorm:"size:16;not null"`
	AmountMinor       int64        `json:"amount_minor,string" gorm:"type:bigint;not null"`
	Currency          string       `json:"currency" gorm:"size:3;not null"`
	ExpectedQuota     common.Quota `json:"expected_quota" gorm:"type:bigint;not null"`
	UnitPrice         string       `json:"unit_price" gorm:"size:64;not null"`
	GroupRatio        string       `json:"group_ratio" gorm:"size:64;not null"`
	Discount          string       `json:"discount" gorm:"size:64;not null"`
	QuoteFingerprint  string       `json:"quote_fingerprint" gorm:"size:64;not null"`
	NextCheckAt       int64        `json:"-" gorm:"index;not null;default:0"`
	CheckedAt         int64        `json:"checked_at" gorm:"not null;default:0"`
	CheckAttempts     int          `json:"-" gorm:"not null;default:0"`
	LeaseToken        string       `json:"-" gorm:"size:36"`
	LeaseUntil        int64        `json:"-" gorm:"not null;default:0"`
	LastCheckError    string       `json:"last_check_error,omitempty" gorm:"size:64"`
}

type EpayPayment struct {
	OrderNo           string
	MerchantID        string
	MerchantVersionID string
	GatewayTradeNo    string
	AmountMinor       int64
	PaymentMethod     string
}

var ErrEpayPaymentMismatch = errors.New("payment does not match the order")
var ErrEpaySnapshotMissing = errors.New("epay order snapshot is missing")
var ErrEpayCheckThrottled = errors.New("payment check is already running or was requested recently")
var ErrEpayMerchantUnavailable = errors.New("epay merchant version is unavailable")
var ErrEpaySettlementPaused = errors.New("epay settlement is paused")

func CreateEpayOrder(ctx context.Context, topUp *TopUp, snapshot *EpayOrder, merchant *EpayMerchantVersion) error {
	if topUp == nil || snapshot == nil || merchant == nil || topUp.Id != 0 || topUp.PaymentProvider != PaymentProviderEpay || snapshot.ExpectedQuota <= 0 || snapshot.AmountMinor <= 0 || snapshot.Currency != "CNY" || merchant.Key == "" || merchant.MerchantID == "" {
		return ErrEpayPaymentMismatch
	}
	account, err := common.Marshal([]string{merchant.GatewayURL, merchant.MerchantID})
	if err != nil {
		return err
	}
	accountHash := sha256.Sum256(account)
	merchant.AccountID = hex.EncodeToString(accountHash[:])
	identity, err := common.Marshal([]string{merchant.GatewayURL, merchant.MerchantID, merchant.Key})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(identity)
	merchant.Fingerprint = hex.EncodeToString(digest[:])
	merchant.ID = uuid.NewString()
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		// SQL diagnostics must never render the credential insert, even on error.
		if err := tx.Session(&gorm.Session{Logger: gormlogger.Discard}).Clauses(clause.OnConflict{DoNothing: true}).Create(merchant).Error; err != nil {
			return ErrEpayMerchantUnavailable
		}
		// Read into an empty record: GORM also filters by a populated primary key,
		// which is different when this insert reused an existing version.
		var savedMerchant EpayMerchantVersion
		if err := tx.Session(&gorm.Session{Logger: gormlogger.Discard}).Where("fingerprint = ?", merchant.Fingerprint).First(&savedMerchant).Error; err != nil {
			return ErrEpayMerchantUnavailable
		}
		*merchant = savedMerchant
		topUp.Id = 0
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		snapshot.TopUpID = topUp.Id
		snapshot.MerchantVersionID = merchant.ID
		snapshot.AccountID = merchant.AccountID
		snapshot.NextCheckAt = topUp.CreateTime + 60
		return tx.Create(snapshot).Error
	})
}

func GetEpayOrder(ctx context.Context, orderNo string) (*TopUp, *EpayOrder, *EpayMerchantVersion, error) {
	topUp := &TopUp{}
	if err := DB.WithContext(ctx).Where("trade_no = ?", orderNo).First(topUp).Error; err != nil {
		return nil, nil, nil, err
	}
	if topUp.PaymentProvider != PaymentProviderEpay {
		return nil, nil, nil, ErrPaymentMethodMismatch
	}
	snapshot := &EpayOrder{}
	if err := DB.WithContext(ctx).First(snapshot, topUp.Id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrEpaySnapshotMissing
		}
		return nil, nil, nil, err
	}
	merchant, err := loadEpayMerchantVersion(DB.WithContext(ctx), snapshot.MerchantVersionID)
	if err != nil {
		return nil, nil, nil, err
	}
	return topUp, snapshot, merchant, nil
}

func loadEpayMerchantVersion(tx *gorm.DB, id string) (*EpayMerchantVersion, error) {
	var merchant EpayMerchantVersion
	if err := tx.Session(&gorm.Session{Logger: gormlogger.Discard}).Where("id = ?", id).First(&merchant).Error; err != nil {
		return nil, ErrEpayMerchantUnavailable
	}
	return &merchant, nil
}

func ClaimEpayCheck(ctx context.Context, id int, now int64) (string, error) {
	token := uuid.NewString()
	result := DB.WithContext(ctx).Model(&EpayOrder{}).Where("top_up_id = ? AND lease_until <= ? AND checked_at <= ?", id, now, now-60).
		Updates(map[string]any{"lease_token": token, "lease_until": now + 30, "checked_at": now})
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected != 1 {
		return "", ErrEpayCheckThrottled
	}
	return token, nil
}

func FinishEpayCheck(ctx context.Context, id int, token string, errorCode string, now int64) error {
	return BillingTransaction(ctx, func(tx *gorm.DB) error {
		var order EpayOrder
		if err := lockForUpdate(tx).Where("top_up_id = ? AND lease_token = ?", id, token).First(&order).Error; err != nil {
			return err
		}
		delays := []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
		index := order.CheckAttempts
		if index >= len(delays) {
			index = len(delays) - 1
		}
		return tx.Model(&order).Updates(map[string]any{"check_attempts": order.CheckAttempts + 1, "next_check_at": now + int64(delays[index].Seconds()), "last_check_error": errorCode, "lease_token": "", "lease_until": 0}).Error
	})
}

func PendingEpayChecks(ctx context.Context, now int64, limit int) ([]TopUp, error) {
	var orders []TopUp
	err := DB.WithContext(ctx).Model(&TopUp{}).Select("top_ups.*").Joins("JOIN epay_orders ON epay_orders.top_up_id = top_ups.id").
		Where("top_ups.payment_provider = ? AND top_ups.status = ? AND top_ups.create_time >= ? AND epay_orders.next_check_at <= ? AND epay_orders.lease_until <= ? AND epay_orders.checked_at <= ?", PaymentProviderEpay, common.TopUpStatusPending, now-7*24*3600, now, now, now-60).
		Order("epay_orders.next_check_at ASC").Limit(limit).Find(&orders).Error
	return orders, err
}
