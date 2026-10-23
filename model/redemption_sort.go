package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RedemptionSort struct {
	column string
	asc    bool
}

// ParseRedemptionSort limits ordering to public fields; request values never
// become SQL fragments. The zero value preserves NewAPI's default ID order.
func ParseRedemptionSort(field, order string) (RedemptionSort, error) {
	columns := map[string]string{"": "id", "id": "id", "created_time": "created_time", "used_time": "redeemed_time"}
	column, ok := columns[field]
	if !ok {
		return RedemptionSort{}, common.NewMessage("invalid redemption sort field")
	}
	if order != "" && order != "asc" && order != "desc" {
		return RedemptionSort{}, common.NewMessage("invalid redemption sort order")
	}
	return RedemptionSort{column: column, asc: order == "asc"}, nil
}

func (sort RedemptionSort) apply(query *gorm.DB) *gorm.DB {
	column := sort.column
	if column == "" {
		column = "id"
	}
	query = query.Order(clause.OrderByColumn{Column: clause.Column{Name: column}, Desc: !sort.asc})
	if column != "id" {
		// Equal timestamps must keep a stable order across pagination.
		query = query.Order(clause.OrderByColumn{Column: clause.Column{Name: "id"}, Desc: !sort.asc})
	}
	return query
}
