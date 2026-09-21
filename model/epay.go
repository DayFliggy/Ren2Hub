package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

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

// Quote fields are immutable. Only the gateway receipt changes after creation.
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
