package model

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	// ErrTopUpReceiptMismatch 表示已验签回调的币种或金额与订单冻结的应付值不一致。
	ErrTopUpReceiptMismatch = errors.New("payment receipt does not match the order")
	// ErrTopUpTypeUnsupported 表示回调属于本地已退役的订单类型（如订阅）。
	ErrTopUpTypeUnsupported = errors.New("payment order type is not supported")
)

// TopUpReceipt 是回调中已验签的应付币种和金额，Currency 为大写 ISO 代码。
type TopUpReceipt struct {
	Currency string
	Amount   string
}

// matchTopUpReceipt 用已验签回执核对订单创建时冻结的 Currency/PayAmount。
// 没有冻结应付值的订单无法核对，同样拒绝，由管理员核实后补单。
func matchTopUpReceipt(order *TopUp, receipt *TopUpReceipt) error {
	due, err := decimal.NewFromString(order.PayAmount)
	if err != nil || !due.IsPositive() || order.Currency == "" {
		return ErrTopUpReceiptMismatch
	}
	paid, err := decimal.NewFromString(receipt.Amount)
	if err != nil || !paid.Equal(due) || receipt.Currency != order.Currency {
		return ErrTopUpReceiptMismatch
	}
	return nil
}

// TopUpCallbackRetryable 是支付回调“错误→是否可重试”的唯一映射。永久错误重试也不会成功：
// 订单不存在、渠道或类型不符、回执不符、订单状态不允许、用户已删除；订单保持未完成，由管理员核实。
// 其余错误（数据库不可用、钱包暂时放不下等）让支付平台稍后重试。
func TopUpCallbackRetryable(err error) bool {
	for _, permanent := range []error{
		ErrTopUpNotFound, ErrPaymentMethodMismatch, ErrTopUpStatusInvalid, ErrTopUpReceiptMismatch,
		ErrTopUpTypeUnsupported, ErrInvalidTopUpQuota, ErrGmpayPaymentMismatch, ErrGmpaySnapshotMissing,
		gorm.ErrRecordNotFound,
	} {
		if errors.Is(err, permanent) {
			return false
		}
	}
	return true
}

// RecordTopUpCallbackRejection 记录一次已验签但被永久拒绝、没有入账的回调。
// 调用方是支付平台而不是站内用户，ActorRole 留空，记录只对 Root 可见。
func RecordTopUpCallbackRejection(callerIP, provider, tradeNo string, userID int, reason error) {
	params := map[string]any{"provider": provider, "trade_no": tradeNo, "reason": reason.Error()}
	RecordAuditLog(nil, AuditLog{
		UserId: userID, Category: AuditCategorySecurity, Action: "payment.callback_rejected", Ip: callerIP,
		Content: "Payment callback rejected without crediting", Status: 200,
		Other: AuditOther{Op: &AuditOperation{Action: "payment.callback_rejected", Params: params}},
	})
}

// topUpSettlement 描述一次入账尝试。渠道只提供自己的差异：mutate 在订单行锁内、
// provider 校验之后运行，可以改写订单字段或拒绝回执，并返回需要随额度一起写入用户表的列。
type topUpSettlement struct {
	tradeNo string
	// provider 为空表示管理员补单，接受任意渠道的订单；人工补单由审计中间件记录。
	provider string
	// receipt 为 nil 表示该渠道的实付金额语义尚未核实，不做金额核对。
	receipt   *TopUpReceipt
	callerIP  string
	logMethod string
	mutate    func(tx *gorm.DB, order *TopUp) (map[string]any, error)
	message   func(order *TopUp, quota int) *common.Message
}

// settleTopUp 是 completeTopUpTx 外唯一的入账外壳：加锁取单、校验渠道、执行渠道 mutate，
// 再交给 completeTopUpTx 完成状态迁移、钱包和返利。mutate 对已成功的订单同样执行，
// 以便迟到回调补记回执；之后已成功的订单直接返回。返回的 quota 为 0 表示订单此前已完成，本次未改动。
func settleTopUp(s topUpSettlement) (*TopUp, int, error) {
	if s.tradeNo == "" {
		return nil, 0, ErrTopUpNotFound
	}
	order := &TopUp{}
	quota := 0
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("trade_no = ?", s.tradeNo).First(order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTopUpNotFound
			}
			return err
		}
		if s.provider != "" && order.PaymentProvider != s.provider {
			return ErrPaymentMethodMismatch
		}
		var updates map[string]any
		if s.mutate != nil {
			var err error
			if updates, err = s.mutate(tx, order); err != nil {
				return err
			}
		}
		// 重复回调同样核对，已完成的订单不接受另一份回执。
		if s.receipt != nil {
			if err := matchTopUpReceipt(order, s.receipt); err != nil {
				return err
			}
		}
		if order.Status == common.TopUpStatusSuccess {
			return nil
		}
		fallback := order.ExpectedQuota
		if fallback <= 0 && order.Status == common.TopUpStatusPending {
			var err error
			if fallback, err = legacyTopUpQuota(order); err != nil {
				return err
			}
		}
		var err error
		quota, err = completeTopUpTx(tx, order, fallback, updates)
		return err
	})
	if err != nil {
		if s.provider != "" && !TopUpCallbackRetryable(err) {
			RecordTopUpCallbackRejection(s.callerIP, s.provider, s.tradeNo, order.UserId, err)
		}
		return nil, 0, err
	}
	if quota > 0 {
		syncCreditUserQuotaCache(order.UserId, quota, common.LogText("%s topup", s.logMethod))
		RecordTopupLog(order.UserId, s.message(order, quota), s.callerIP, order.PaymentMethod, s.logMethod)
	}
	return order, quota, nil
}

var topUpCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func validTopUpCurrency(value string) bool { return topUpCurrencyPattern.MatchString(value) }

func legacyTopUpQuota(order *TopUp) (int, error) {
	amount := decimal.NewFromInt(order.Amount)
	if order.PaymentProvider == PaymentProviderStripe {
		amount = decimal.NewFromFloat(order.Money)
	}
	if order.PaymentProvider != PaymentProviderCreem {
		amount = amount.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	quota, err := common.WalletQuotaFromDecimalStrict(amount)
	if err != nil || quota <= 0 {
		return 0, ErrInvalidTopUpQuota
	}
	return quota, nil
}

// FreezeTopUpQuota records the exact paid credit before the order is sent.
// Discounts change the checkout price; no implicit bonus quota is invented.
func FreezeTopUpQuota(order *TopUp) error {
	if order.ExpectedQuota <= 0 {
		quota, err := legacyTopUpQuota(order)
		if err != nil {
			return err
		}
		order.ExpectedQuota = quota
	}
	if _, err := topUpQuotaMaxCurrent(order.ExpectedQuota); err != nil {
		return err
	}
	order.AffiliateBaseQuota = order.ExpectedQuota
	order.Currency = strings.ToUpper(strings.TrimSpace(order.Currency))
	if order.Currency != "" && !validTopUpCurrency(order.Currency) {
		return fmt.Errorf("invalid order currency")
	}
	if order.PayAmount == "" && order.Currency != "" {
		order.PayAmount = decimal.NewFromFloat(order.Money).String()
	}
	if order.PayAmount != "" {
		value, err := decimal.NewFromString(order.PayAmount)
		if err != nil || !value.IsPositive() {
			return ErrInvalidTopUpQuota
		}
		order.PayAmount = value.String()
		if len(order.PayAmount) > 64 {
			return fmt.Errorf("order payment amount is too large")
		}
	}
	return nil
}

// completeTopUpTx is the sole successful TopUp transition. The caller holds the
// order lock and has already verified its provider receipt. Referral credit and
// wallet credit either commit together, or both roll back.
func completeTopUpTx(tx *gorm.DB, order *TopUp, fallbackQuota int, updates map[string]any) (int, error) {
	if order.Status == common.TopUpStatusSuccess {
		return 0, nil
	}
	if order.Status != common.TopUpStatusPending {
		return 0, ErrTopUpStatusInvalid
	}
	quota := order.ExpectedQuota
	if quota <= 0 {
		quota = fallbackQuota
		// Pending native orders created before these snapshot columns still
		// earn under the rules at their first successful credit, never replay.
		order.ExpectedQuota = quota
		order.AffiliateBaseQuota = quota
	}
	if _, err := topUpQuotaMaxCurrent(quota); err != nil {
		return 0, err
	}
	user, err := lockAffiliateParticipantsTx(tx, order.UserId)
	if err != nil {
		return 0, err
	}
	if err = creditTopUpQuota(tx, order.UserId, quota, updates); err != nil {
		return 0, err
	}
	base := order.AffiliateBaseQuota
	if base < 0 || base > quota {
		return 0, ErrInvalidTopUpQuota
	}
	if err = applyAffiliateRewardTx(tx, user, "topup", fmt.Sprint(order.Id), base); err != nil {
		return 0, err
	}
	order.CompleteTime = common.GetTimestamp()
	order.Status = common.TopUpStatusSuccess
	order.CreditedQuota = quota
	if err = tx.Save(order).Error; err != nil {
		return 0, err
	}
	return quota, nil
}
