package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// TopUpDiscount freezes the discount decision of one top-up order for
// administrators. Settlement never reads it: the order's PayAmount and
// ExpectedQuota are the only values a callback is checked against.
type TopUpDiscount struct {
	TopUpID       int    `json:"top_up_id" gorm:"column:top_up_id;primaryKey;autoIncrement:false"`
	ConfigVersion int64  `json:"config_version" gorm:"not null"`
	ListPayAmount string `json:"list_pay_amount" gorm:"type:varchar(64);not null"`
	Decision      string `json:"decision" gorm:"type:text;not null"`
}

func (TopUpDiscount) TableName() string {
	return "topup_discounts"
}

// insertTopUpDiscountTx stores the order's decision in the order's own
// transaction. Orders priced without a discount (Stripe, Creem) carry none.
func insertTopUpDiscountTx(tx *gorm.DB, order *TopUp) error {
	if order.Discount == nil {
		return nil
	}
	order.Discount.TopUpID = order.Id
	return tx.Create(order.Discount).Error
}

// GetTopUpDiscountRecord returns the frozen decision of an order, if any.
func GetTopUpDiscountRecord(topUpID int) (*TopUpDiscount, error) {
	var rows []TopUpDiscount
	if err := DB.Where("top_up_id = ?", topUpID).Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// SuccessfulTopUpsBetween lists successful orders of the given providers
// created in [start, end), for the discount estimate.
func SuccessfulTopUpsBetween(start, end int64, providers []string) ([]TopUp, error) {
	var rows []TopUp
	err := DB.Select("id", "user_id", "create_time", "expected_quota").
		Where("status = ? AND create_time >= ? AND create_time < ? AND payment_provider IN ?", common.TopUpStatusSuccess, start, end, providers).
		Find(&rows).Error
	return rows, err
}
