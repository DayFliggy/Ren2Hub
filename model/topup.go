package model

import (
	"errors"
	"fmt"
	"maps"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"gorm.io/gorm"
)

type TopUp struct {
	Id                 int     `json:"id"`
	UserId             int     `json:"user_id" gorm:"index"`
	Amount             int64   `json:"amount"`
	Money              float64 `json:"money"`
	TradeNo            string  `json:"trade_no" gorm:"unique;type:varchar(255);index"`
	PaymentMethod      string  `json:"payment_method" gorm:"type:varchar(71)"`
	PaymentProvider    string  `json:"payment_provider" gorm:"type:varchar(50);default:''"`
	CreateTime         int64   `json:"create_time"`
	CompleteTime       int64   `json:"complete_time"`
	Status             string  `json:"status"`
	Currency           string  `json:"currency" gorm:"type:varchar(3)"`
	PayAmount          string  `json:"pay_amount" gorm:"type:varchar(64)"`
	ExpectedQuota      int     `json:"expected_quota" gorm:"type:bigint;not null;default:0"`
	CreditedQuota      int     `json:"credited_quota" gorm:"type:bigint;not null;default:0"`
	AffiliateBaseQuota int     `json:"affiliate_base_quota" gorm:"type:bigint;not null;default:0"`
	// BonusQuota is the frozen top-up bonus included in ExpectedQuota.
	BonusQuota int `json:"bonus_quota" gorm:"type:bigint;not null;default:0"`
	// Bonus is the decision written with the order and never serialized with it.
	Bonus *TopUpBonusGrant `json:"-" gorm:"-"`
}

const (
	PaymentMethodStripe       = "stripe"
	PaymentMethodCreem        = "creem"
	PaymentMethodWaffo        = "waffo"
	PaymentMethodWaffoPancake = "waffo_pancake"
	PaymentMethodBalance      = "balance"
)

const (
	PaymentProviderGmpay        = "gmpay"
	PaymentProviderEpay         = "epay"
	PaymentProviderStripe       = "stripe"
	PaymentProviderCreem        = "creem"
	PaymentProviderWaffo        = "waffo"
	PaymentProviderWaffoPancake = "waffo_pancake"
	PaymentProviderBalance      = "balance"
)

var (
	ErrPaymentMethodMismatch = errors.New("payment method mismatch")
	ErrTopUpNotFound         = errors.New("topup not found")
	ErrTopUpStatusInvalid    = errors.New("topup status invalid")
	// The quota errors reach the web console, so they carry message keys.
	ErrInvalidTopUpQuota        error = common.NewMessage("Invalid top-up quota")
	ErrTopUpQuotaLimitExceeded  error = common.NewMessage("Top-up quota limit exceeded")
	ErrWalletQuotaLimitExceeded error = common.NewMessage("Wallet quota limit exceeded")
)

func (topUp *TopUp) Insert() error {
	if err := FreezeTopUpQuota(topUp); err != nil {
		return err
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(topUp).Error; err != nil {
			return err
		}
		return insertTopUpBonusTx(tx, topUp)
	})
}

func topUpQuotaMaxCurrent(creditedQuota int) (int, error) {
	if creditedQuota <= 0 || creditedQuota > common.MaxWalletQuota {
		return 0, ErrInvalidTopUpQuota
	}
	return common.MaxWalletQuota - creditedQuota, nil
}

// ValidateTopUpQuotaCapacity performs the user-facing pre-payment check. The
// settlement path repeats the same invariant with an atomic conditional
// update, because the wallet balance can change after checkout creation.
func ValidateTopUpQuotaCapacity(userId int, creditedQuota int) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	var user User
	if err := DB.Select("quota").Where("id = ?", userId).First(&user).Error; err != nil {
		return err
	}
	if user.Quota > maxCurrentQuota {
		return ErrTopUpQuotaLimitExceeded
	}
	return nil
}

// creditTopUpQuota atomically enforces the wallet ceiling while adding quota.
// Keeping the predicate and increment in one UPDATE prevents two
// concurrent callbacks from both passing a separate read/check.
func creditTopUpQuota(tx *gorm.DB, userId int, creditedQuota int, updates map[string]any) error {
	maxCurrentQuota, err := topUpQuotaMaxCurrent(creditedQuota)
	if err != nil {
		return err
	}

	updateFields := make(map[string]any, len(updates)+1)
	maps.Copy(updateFields, updates)
	updateFields["quota"] = gorm.Expr("quota + ?", creditedQuota)

	result := tx.Model(&User{}).
		Where("id = ? AND quota <= ?", userId, maxCurrentQuota).
		Updates(updateFields)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}

	var count int64
	if err := tx.Model(&User{}).Where("id = ?", userId).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return gorm.ErrRecordNotFound
	}
	return ErrTopUpQuotaLimitExceeded
}

func (topUp *TopUp) Update() error {
	return UpdatePendingTopUpStatus(topUp.TradeNo, topUp.PaymentProvider, topUp.Status)
}

func GetTopUpById(id int) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("id = ?", id).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func GetTopUpByTradeNo(tradeNo string) *TopUp {
	var topUp *TopUp
	var err error
	err = DB.Where("trade_no = ?", tradeNo).First(&topUp).Error
	if err != nil {
		return nil
	}
	return topUp
}

func UpdatePendingTopUpStatus(tradeNo string, expectedPaymentProvider string, targetStatus string) error {
	if targetStatus != common.TopUpStatusFailed && targetStatus != common.TopUpStatusExpired {
		return ErrTopUpStatusInvalid
	}
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}

	refCol := "`trade_no`"
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		refCol = `"trade_no"`
	}

	return DB.Transaction(func(tx *gorm.DB) error {
		topUp := &TopUp{}
		if err := lockForUpdate(tx).Where(refCol+" = ?", tradeNo).First(topUp).Error; err != nil {
			return ErrTopUpNotFound
		}
		if expectedPaymentProvider != "" && topUp.PaymentProvider != expectedPaymentProvider {
			return ErrPaymentMethodMismatch
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}

		topUp.Status = targetStatus
		return tx.Save(topUp).Error
	})
}

// RechargeEpay 原子完成易支付订单：订单行锁、状态校验、成功更新与用户额度增加
// 在同一个事务内完成，因此同一订单的并发/重复回调（包括多实例部署下）最多充值一次。
// alreadyDone=true 表示订单此前已完成，本次为幂等重复回调。
// 进程内的 LockOrder 只是优化，正确性由本函数的数据库行锁保证。
// paidMoney 是回调中已验签的 money（人民币元），必须等于下单时冻结的应付金额。
func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string, paidMoney string) (alreadyDone bool, err error) {
	if tradeNo == "" {
		return false, errors.New("payment order number not provided")
	}
	_, quota, err := settleTopUp(topUpSettlement{
		tradeNo: tradeNo, provider: PaymentProviderEpay, callerIP: callerIp, logMethod: PaymentProviderEpay,
		receipt: &TopUpReceipt{Currency: "CNY", Amount: paidMoney},
		mutate: func(_ *gorm.DB, order *TopUp) (map[string]any, error) {
			if actualPaymentMethod != "" {
				order.PaymentMethod = actualPaymentMethod
			}
			return nil, nil
		},
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%f", order.Money)})
		},
	})
	return err == nil && quota == 0, err
}

func Recharge(referenceId string, customerId string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment order number not provided")
	}
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: referenceId, provider: PaymentProviderStripe, callerIP: callerIp, logMethod: PaymentMethodStripe,
		mutate: func(_ *gorm.DB, order *TopUp) (map[string]any, error) {
			if order.Status != common.TopUpStatusPending {
				return nil, errors.New("invalid top-up order status")
			}
			return map[string]any{"stripe_customer": customerId}, nil
		},
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quota), "amount": order.Amount})
		},
	})
	if err != nil {
		common.SysError(common.LogText("topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	return nil
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// ManualCompleteTopUp 管理员手动完成订单并给用户充值
func ManualCompleteTopUp(tradeNo string, callerIp string) error {
	if tradeNo == "" {
		return common.NewMessage("Order number not provided")
	}
	_, _, err := settleTopUp(topUpSettlement{
		tradeNo: tradeNo, callerIP: callerIp, logMethod: "admin",
		mutate: func(tx *gorm.DB, order *TopUp) (map[string]any, error) {
			if order.PaymentProvider != PaymentProviderGmpay || order.Status != common.TopUpStatusPending {
				return nil, nil
			}
			return nil, settleGmpaySnapshotByAdmin(tx, order)
		},
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("Administrator completed the order, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%f", order.Money)})
		},
	})
	switch {
	case errors.Is(err, ErrTopUpNotFound):
		return common.NewMessage("Top-up order does not exist")
	case errors.Is(err, ErrTopUpStatusInvalid):
		return common.NewMessage("Order is not pending payment and cannot be completed")
	case errors.Is(err, ErrGmpayStateConflict):
		return common.NewMessage("The payment gateway rejected or already settled this order and it cannot be completed")
	}
	return err
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	if referenceId == "" {
		return errors.New("payment order number not provided")
	}
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: referenceId, provider: PaymentProviderCreem, callerIP: callerIp, logMethod: PaymentMethodCreem,
		mutate: func(tx *gorm.DB, order *TopUp) (map[string]any, error) {
			if order.Status != common.TopUpStatusPending {
				return nil, errors.New("invalid top-up order status")
			}
			// 用户邮箱为空时，补记支付时使用的邮箱
			if customerEmail == "" {
				return nil, nil
			}
			var user User
			if err := tx.Where("id = ?", order.UserId).First(&user).Error; err != nil {
				return nil, err
			}
			if user.Email != "" {
				return nil, nil
			}
			return map[string]any{"email": customerEmail}, nil
		},
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Creem", "quota": quota, "amount": fmt.Sprintf("%.2f", order.Money)})
		},
	})
	if err != nil {
		common.SysError(common.LogText("creem topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	return nil
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: tradeNo, provider: PaymentProviderWaffo, callerIP: callerIp, logMethod: PaymentMethodWaffo,
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Waffo", "quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%.2f", order.Money)})
		},
	})
	if err != nil {
		common.SysError(common.LogText("waffo topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	return nil
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	if tradeNo == "" {
		return errors.New("payment order number not provided")
	}
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: tradeNo, provider: PaymentProviderWaffoPancake, logMethod: PaymentMethodWaffoPancake,
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Waffo Pancake", "quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%.2f", order.Money)})
		},
	})
	if err != nil {
		common.SysError(common.LogText("waffo pancake topup failed: %s", err.Error()))
		return errors.New("top-up failed")
	}
	return nil
}
