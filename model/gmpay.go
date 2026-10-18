package model

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/pkg/gmpay"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

// GmpayOrderStatus tracks where an order stands between the local snapshot and
// the gateway call. The gateway's create is not idempotent, so an outcome that
// cannot be determined must stay visible instead of silently retrying.
const (
	GmpayStatusCreating = "creating"
	GmpayStatusReady    = "ready"
	GmpayStatusFailed   = "failed"
	GmpayStatusUnknown  = "unknown"
	// GmpayStatusSettled is written by settlement, which may run before the
	// creation call returns. A late result may add receipt evidence without
	// reopening an order that already credited the wallet.
	GmpayStatusSettled = "settled"
)

var (
	ErrGmpayPaymentMismatch     = errors.New("payment does not match the order")
	ErrGmpaySnapshotMissing     = errors.New("gmpay order snapshot is missing")
	ErrGmpayMerchantUnavailable = errors.New("gmpay merchant version is unavailable")
	ErrGmpayStateConflict       = errors.New("gmpay order state conflict")
)

// GmpayMerchantVersion is an immutable credential version. Every field is
// private to the process: credentials must never reach an API response.
type GmpayMerchantVersion struct {
	ID          string `json:"-" gorm:"primaryKey;size:36"`
	Fingerprint string `json:"-" gorm:"uniqueIndex;size:64"`
	AccountID   string `json:"-" gorm:"size:64;not null"`
	GatewayURL  string `json:"-" gorm:"type:text;not null"`
	MerchantID  string `json:"-" gorm:"size:128;not null"`
	Key         string `json:"-" gorm:"type:text;not null"`
}

// GmpayOrder records the native credited quota and payment amount at creation. Only the gateway receipt and the
// creation state change after creation; nothing here is ever overwritten.
type GmpayOrder struct {
	TopUpID           int     `json:"-" gorm:"primaryKey;autoIncrement:false"`
	MerchantVersionID string  `json:"-" gorm:"size:36;not null"`
	ReceiptScope      string  `json:"-" gorm:"size:64;not null;default:'gmpay';uniqueIndex:idx_gmpay_receipt,priority:1"`
	AccountID         string  `json:"-" gorm:"size:64;not null"`
	GatewayTradeNo    *string `json:"gateway_trade_no,omitempty" gorm:"size:128;uniqueIndex:idx_gmpay_receipt,priority:2"`
	AmountMinor       int64   `json:"amount_minor,string" gorm:"type:bigint;not null"`
	Currency          string  `json:"currency" gorm:"size:3;not null"`
	ExpectedQuota     int     `json:"expected_quota" gorm:"type:bigint;not null"`
	Token             string  `json:"token" gorm:"size:32;not null"`
	Network           string  `json:"network" gorm:"size:32;not null"`
	Status            string  `json:"status" gorm:"size:16;not null;default:'creating'"`
	LastFailure       string  `json:"last_failure" gorm:"size:64;not null;default:''"`
	GatewayAmount     string  `json:"gateway_amount" gorm:"size:64;not null;default:''"`
	GatewayCryptoAmt  string  `json:"gateway_crypto_amount" gorm:"size:64;not null;default:''"`
	ReceiveAddress    string  `json:"receive_address" gorm:"size:256;not null;default:''"`
	ExpirationTime    int64   `json:"expiration_time" gorm:"type:bigint;not null;default:0"`
	// Final payment evidence can differ from the create response when the
	// gateway cashier switches the payment network.
	SettledCryptoAmount   string `json:"settled_crypto_amount" gorm:"size:64;not null;default:''"`
	SettledReceiveAddress string `json:"settled_receive_address" gorm:"size:256;not null;default:''"`
	SettledTransactionID  string `json:"settled_transaction_id" gorm:"size:128;not null;default:''"`
}

// GmpayPayment carries a verified gateway notification into settlement.
type GmpayPayment struct {
	OrderNo            string
	MerchantID         string
	MerchantVersionID  string
	GatewayTradeNo     string
	AmountMinor        int64
	Token              string
	ActualAmount       string
	ReceiveAddress     string
	BlockTransactionID string
}

// GmpayOrderResult records a validated create response or a bounded failure.
// The trade number is bound before returning a checkout URL to the caller.
type GmpayOrderResult struct {
	Status              string
	GatewayTradeNo      string
	GatewayAmount       string
	GatewayCryptoAmount string
	ReceiveAddress      string
	ExpirationTime      int64
	Failure             string
}

// CreateGmpayOrder stores the credential version, the local order and the
// frozen snapshot in one transaction, before any gateway call. The receipt
// scope makes (scope, gateway trade number) unique, so one gateway trade cannot
// be claimed by a second local order.
func CreateGmpayOrder(ctx context.Context, topUp *TopUp, snapshot *GmpayOrder, merchant *GmpayMerchantVersion) error {
	if topUp == nil || snapshot == nil || merchant == nil || topUp.Id != 0 || topUp.PaymentProvider != PaymentProviderGmpay ||
		snapshot.ExpectedQuota <= 0 || snapshot.ExpectedQuota > common.MaxWalletQuota || snapshot.AmountMinor <= 0 || snapshot.Currency != "CNY" ||
		merchant.Key == "" || merchant.MerchantID == "" || snapshot.Token == "" || snapshot.Network == "" {
		return ErrGmpayPaymentMismatch
	}
	topUp.Currency = snapshot.Currency
	topUp.PayAmount = decimal.NewFromInt(snapshot.AmountMinor).Div(decimal.NewFromInt(100)).String()
	topUp.ExpectedQuota = snapshot.ExpectedQuota
	if err := FreezeTopUpQuota(topUp); err != nil {
		return err
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
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// SQL diagnostics must never render the credential insert.
		if err := tx.Session(&gorm.Session{Logger: gormlogger.Discard}).Clauses(clause.OnConflict{DoNothing: true}).Create(merchant).Error; err != nil {
			return ErrGmpayMerchantUnavailable
		}
		// Read into an empty record: GORM also filters by a populated primary
		// key, which differs when this insert reused an existing version.
		var saved GmpayMerchantVersion
		if err := tx.Session(&gorm.Session{Logger: gormlogger.Discard}).Where("fingerprint = ?", merchant.Fingerprint).First(&saved).Error; err != nil {
			return ErrGmpayMerchantUnavailable
		}
		*merchant = saved
		topUp.Id = 0
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		snapshot.TopUpID = topUp.Id
		snapshot.MerchantVersionID = merchant.ID
		snapshot.AccountID = merchant.AccountID
		snapshot.ReceiptScope = "gmpay"
		if snapshot.Status == "" {
			snapshot.Status = GmpayStatusCreating
		}
		return tx.Create(snapshot).Error
	})
}

// GetGmpayOrder resolves the local order, its frozen snapshot and the bound
// credential version.
func GetGmpayOrder(ctx context.Context, orderNo string) (*TopUp, *GmpayOrder, *GmpayMerchantVersion, error) {
	topUp := &TopUp{}
	if err := DB.WithContext(ctx).Where("trade_no = ?", orderNo).First(topUp).Error; err != nil {
		return nil, nil, nil, err
	}
	if topUp.PaymentProvider != PaymentProviderGmpay {
		return nil, nil, nil, ErrPaymentMethodMismatch
	}
	snapshot := &GmpayOrder{}
	if err := DB.WithContext(ctx).First(snapshot, topUp.Id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, nil, ErrGmpaySnapshotMissing
		}
		return nil, nil, nil, err
	}
	merchant, err := loadGmpayMerchantVersion(DB.WithContext(ctx), snapshot.MerchantVersionID)
	if err != nil {
		return nil, nil, nil, err
	}
	return topUp, snapshot, merchant, nil
}

func loadGmpayMerchantVersion(tx *gorm.DB, id string) (*GmpayMerchantVersion, error) {
	var merchant GmpayMerchantVersion
	if err := tx.Session(&gorm.Session{Logger: gormlogger.Discard}).Where("id = ?", id).First(&merchant).Error; err != nil {
		return nil, ErrGmpayMerchantUnavailable
	}
	return &merchant, nil
}

// MarkGmpayOrderResult records the outcome of the gateway call. A ready order
// stores the receipt evidence; a failed or undetermined order keeps its local
// number so an operator can review it.
func MarkGmpayOrderResult(ctx context.Context, topUpID int, result GmpayOrderResult) error {
	if topUpID <= 0 {
		return ErrGmpaySnapshotMissing
	}
	switch result.Status {
	case GmpayStatusReady, GmpayStatusFailed, GmpayStatusUnknown:
	default:
		return ErrGmpayStateConflict
	}
	if result.Status == GmpayStatusReady && (result.GatewayTradeNo == "" || len(result.GatewayTradeNo) > 128) {
		return ErrGmpayPaymentMismatch
	}
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var topUp TopUp
		if err := lockForUpdate(tx).First(&topUp, topUpID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		if topUp.PaymentProvider != PaymentProviderGmpay {
			return ErrPaymentMethodMismatch
		}
		snapshot := &GmpayOrder{}
		if err := lockForUpdate(tx).First(snapshot, topUpID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGmpaySnapshotMissing
			}
			return err
		}
		settled := topUp.Status == common.TopUpStatusSuccess
		if !settled && (topUp.Status != common.TopUpStatusPending || snapshot.Status != GmpayStatusCreating) {
			return ErrGmpayStateConflict
		}
		if settled && result.Status != GmpayStatusReady {
			// Payment is stronger evidence than a late transport failure.
			return nil
		}
		updates := map[string]interface{}{"status": result.Status, "last_failure": result.Failure}
		if result.Status == GmpayStatusReady {
			if snapshot.GatewayTradeNo != nil && *snapshot.GatewayTradeNo != result.GatewayTradeNo {
				return ErrGmpayPaymentMismatch
			}
			if snapshot.GatewayAmount != "" {
				if snapshot.GatewayAmount != result.GatewayAmount || snapshot.GatewayCryptoAmt != result.GatewayCryptoAmount ||
					snapshot.ReceiveAddress != result.ReceiveAddress || snapshot.ExpirationTime != result.ExpirationTime {
					return ErrGmpayStateConflict
				}
				return nil
			}
			updates["gateway_trade_no"] = result.GatewayTradeNo
			updates["gateway_amount"] = result.GatewayAmount
			updates["gateway_crypto_amt"] = result.GatewayCryptoAmount
			updates["receive_address"] = result.ReceiveAddress
			updates["expiration_time"] = result.ExpirationTime
		}
		if settled {
			updates["status"] = GmpayStatusSettled
		}
		return tx.Model(snapshot).Updates(updates).Error
	})
}

// RechargeGmpay settles a verified notification. Order row lock, state checks,
// the success transition and native wallet credit share one transaction, so
// concurrent or repeated callbacks credit at most once.
func RechargeGmpay(payment GmpayPayment, callerIp string) (alreadyDone bool, err error) {
	tradeNo := payment.OrderNo
	if tradeNo == "" {
		return false, errors.New("payment order number not provided")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	var quotaToAdd int
	topUp := &TopUp{}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		if topUp.PaymentProvider != PaymentProviderGmpay {
			return ErrPaymentMethodMismatch
		}
		var snapshot GmpayOrder
		if err := lockForUpdate(tx).First(&snapshot, topUp.Id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrGmpaySnapshotMissing
			}
			return err
		}
		merchant, err := loadGmpayMerchantVersion(tx, snapshot.MerchantVersionID)
		if err != nil {
			return err
		}
		if payment.MerchantVersionID != merchant.ID || payment.MerchantID != merchant.MerchantID ||
			payment.AmountMinor != snapshot.AmountMinor || payment.AmountMinor <= 0 ||
			payment.GatewayTradeNo == "" || len(payment.GatewayTradeNo) > 128 ||
			snapshot.ExpectedQuota <= 0 || snapshot.Currency != "CNY" {
			return ErrGmpayPaymentMismatch
		}
		if payment.Token == "" || payment.Token != snapshot.Token {
			return ErrGmpayPaymentMismatch
		}
		actualAmount, err := gmpay.ParseTokenAmount(payment.ActualAmount)
		if err != nil || len(actualAmount.String()) > 64 ||
			payment.ReceiveAddress == "" || len(payment.ReceiveAddress) > 256 ||
			payment.BlockTransactionID == "" || len(payment.BlockTransactionID) > 128 {
			return ErrGmpayPaymentMismatch
		}
		settledAmount := actualAmount.String()
		if snapshot.SettledCryptoAmount != "" || snapshot.SettledReceiveAddress != "" || snapshot.SettledTransactionID != "" {
			if snapshot.SettledCryptoAmount != settledAmount || snapshot.SettledReceiveAddress != payment.ReceiveAddress ||
				snapshot.SettledTransactionID != payment.BlockTransactionID {
				return ErrGmpayPaymentMismatch
			}
		}
		if snapshot.GatewayTradeNo != nil && *snapshot.GatewayTradeNo != payment.GatewayTradeNo {
			return ErrGmpayPaymentMismatch
		}
		// Close the creation window in the same transaction that credits: a
		// callback can arrive before the gateway answers, and the order must not
		// stay creatable once real money has moved. An administrator-completed
		// order can bind its first receipt here without crediting again.
		if err := tx.Model(&snapshot).Updates(map[string]interface{}{
			"status": GmpayStatusSettled, "gateway_trade_no": payment.GatewayTradeNo,
			"settled_crypto_amount": settledAmount, "settled_receive_address": payment.ReceiveAddress,
			"settled_transaction_id": payment.BlockTransactionID,
		}).Error; err != nil {
			return err
		}
		if topUp.Status == common.TopUpStatusSuccess {
			alreadyDone = true
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		quotaToAdd = snapshot.ExpectedQuota
		if quotaToAdd <= 0 {
			return ErrInvalidTopUpQuota
		}
		var completeErr error
		quotaToAdd, completeErr = completeTopUpTx(tx, topUp, quotaToAdd, nil)
		if completeErr != nil {
			return completeErr
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	if alreadyDone {
		return true, nil
	}
	syncCreditUserQuotaCache(topUp.UserId, quotaToAdd, common.LogText("gmpay topup"))
	RecordTopupLog(topUp.UserId, common.NewMessage("Crypto top-up succeeded, amount added: {{quota}}", map[string]any{"quota": logger.FormatQuota(quotaToAdd)}), callerIp, topUp.PaymentMethod, PaymentProviderGmpay)
	return false, nil
}
