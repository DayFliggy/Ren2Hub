package model

import (
	"errors"
	"fmt"
	"maps"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"

	"github.com/shopspring/decimal"
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
	var err error
	err = DB.Create(topUp).Error
	return err
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
func RechargeEpay(tradeNo string, actualPaymentMethod string, callerIp string) (alreadyDone bool, err error) {
	_, quota, err := settleTopUp(topUpSettlement{
		tradeNo: tradeNo, provider: PaymentProviderEpay, callerIP: callerIp, logMethod: PaymentProviderEpay,
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

func Recharge(referenceId string, customerId string, callerIp string) error {
	return RechargeWithPayment(referenceId, customerId, callerIp, "", "")
}
func RechargeWithPayment(referenceId string, customerId string, callerIp string, currency string, payAmount string) (err error) {
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: referenceId, provider: PaymentProviderStripe, callerIP: callerIp, logMethod: PaymentMethodStripe,
		mutate: func(_ *gorm.DB, order *TopUp) (map[string]any, error) {
			if currency != "" {
				if order.Currency != "" && order.Currency != currency {
					return nil, errors.New("payment currency mismatch")
				}
				if !validTopUpCurrency(currency) {
					return nil, errors.New("invalid payment currency")
				}
				value, parseErr := decimal.NewFromString(payAmount)
				if parseErr != nil || value.IsNegative() {
					return nil, errors.New("invalid payment amount")
				}
				order.Currency = currency
				order.PayAmount = value.String()
			}
			return map[string]any{"stripe_customer": customerId}, nil
		},
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"quota": logger.FormatQuota(quota), "amount": order.Amount})
		},
	})
	return err
}

// topUpQueryWindowSeconds 限制充值记录查询的时间窗口（秒）。
const topUpQueryWindowSeconds int64 = 30 * 24 * 60 * 60

// topUpQueryCutoff 返回允许查询的最早 create_time（秒级 Unix 时间戳）。
func topUpQueryCutoff() int64 {
	return common.GetTimestamp() - topUpQueryWindowSeconds
}

func GetUserTopUps(userId int, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	// Start transaction
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	cutoff := topUpQueryCutoff()

	// Get total count within transaction
	err = tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, cutoff).Count(&total).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Get paginated topups within same transaction
	err = tx.Where("user_id = ? AND create_time >= ?", userId, cutoff).Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error
	if err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	// Commit transaction
	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// GetAllTopUps 获取全平台的充值记录（管理员使用，不限制时间窗口）
func GetAllTopUps(pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err = tx.Model(&TopUp{}).Count(&total).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		return nil, 0, err
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}

	return topups, total, nil
}

// searchTopUpCountHardLimit 搜索充值记录时 COUNT 的安全上限，
// 防止对超大表执行无界 COUNT 触发 DoS。
const searchTopUpCountHardLimit = 10000

// SearchUserTopUps 按订单号搜索某用户的充值记录
func SearchUserTopUps(userId int, keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{}).Where("user_id = ? AND create_time >= ?", userId, topUpQueryCutoff())
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to count search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

// SearchAllTopUps 按订单号搜索全平台充值记录（管理员使用，不限制时间窗口）
func SearchAllTopUps(keyword string, pageInfo *common.PageInfo) (topups []*TopUp, total int64, err error) {
	tx := DB.Begin()
	if tx.Error != nil {
		return nil, 0, tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	query := tx.Model(&TopUp{})
	if keyword != "" {
		pattern, perr := sanitizeLikePattern(keyword)
		if perr != nil {
			tx.Rollback()
			return nil, 0, perr
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}

	if err = query.Limit(searchTopUpCountHardLimit).Count(&total).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to count search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	if err = query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Find(&topups).Error; err != nil {
		tx.Rollback()
		common.SysError(common.LogText("failed to search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}

	if err = tx.Commit().Error; err != nil {
		return nil, 0, err
	}
	return topups, total, nil
}

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
	}
	return err
}
func RechargeCreem(referenceId string, customerEmail string, customerName string, callerIp string) (err error) {
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: referenceId, provider: PaymentProviderCreem, callerIP: callerIp, logMethod: PaymentMethodCreem,
		mutate: func(tx *gorm.DB, order *TopUp) (map[string]any, error) {
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
	return err
}

func RechargeWaffo(tradeNo string, callerIp string) (err error) {
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: tradeNo, provider: PaymentProviderWaffo, callerIP: callerIp, logMethod: PaymentMethodWaffo,
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Waffo", "quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%.2f", order.Money)})
		},
	})
	return err
}

func RechargeWaffoPancake(tradeNo string) (err error) {
	_, _, err = settleTopUp(topUpSettlement{
		tradeNo: tradeNo, provider: PaymentProviderWaffoPancake, logMethod: PaymentMethodWaffoPancake,
		message: func(order *TopUp, quota int) *common.Message {
			return common.NewMessage("{{provider}} top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{"provider": "Waffo Pancake", "quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%.2f", order.Money)})
		},
	})
	return err
}
