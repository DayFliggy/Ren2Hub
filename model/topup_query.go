package model

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUpFilter struct {
	UserID                            int
	Keyword, Status, Method, Currency string
	StartTime, EndTime                int64
}

func topUpFilteredQuery(filter TopUpFilter) (*gorm.DB, error) {
	if filter.UserID < 0 || filter.StartTime < 0 || filter.EndTime < 0 {
		return nil, errors.New("invalid topup filter")
	}
	query := DB.Model(&TopUp{})
	if filter.UserID > 0 {
		query = query.Where("user_id = ?", filter.UserID)
	}
	if filter.Keyword != "" {
		query = query.Where("trade_no LIKE ? ESCAPE '!'", "%"+strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(filter.Keyword)+"%")
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Method != "" {
		query = query.Where("(payment_method = ? OR payment_provider = ?)", filter.Method, filter.Method)
	}
	if filter.Currency != "" {
		query = query.Where("currency = ?", strings.ToUpper(filter.Currency))
	}
	start := filter.StartTime
	if start == 0 {
		start = topUpQueryCutoff()
	}
	query = query.Where("create_time >= ?", start)
	if filter.EndTime > 0 {
		if filter.EndTime < start {
			return nil, errors.New("end_time precedes start_time")
		}
		query = query.Where("create_time <= ?", filter.EndTime)
	}
	return query, nil
}
func ListTopUps(filter TopUpFilter, page *common.PageInfo) ([]TopUp, int64, error) {
	query, err := topUpFilteredQuery(filter)
	if err != nil {
		return nil, 0, err
	}
	var rows []TopUp
	var total int64
	if err = query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&rows).Error
	return rows, total, err
}

type TopUpDetail struct {
	*TopUp
	Username string      `json:"username"`
	Email    string      `json:"email"`
	Gmpay    *GmpayOrder `json:"gmpay"`
}

func GetTopUpDetail(tradeNo string, userID int) (*TopUpDetail, error) {
	var row TopUp
	query := DB.Where("trade_no = ?", tradeNo)
	if userID > 0 {
		query = query.Where("user_id = ?", userID)
	}
	if err := query.First(&row).Error; err != nil {
		return nil, err
	}
	result := &TopUpDetail{TopUp: &row}
	var user User
	if err := DB.Unscoped().Select("id", "username", "email").First(&user, row.UserId).Error; err != nil {
		return nil, err
	}
	result.Username, result.Email = user.Username, user.Email
	if row.PaymentProvider == PaymentProviderGmpay {
		var snapshot GmpayOrder
		if err := DB.First(&snapshot, row.Id).Error; err != nil {
			return nil, err
		}
		result.Gmpay = &snapshot
	}
	return result, nil
}

type TopUpCurrencyTotal struct {
	Currency           string `json:"currency"`
	Count              int    `json:"count"`
	PaidCount          int    `json:"paid_count"`
	PaidAmount         string `json:"paid_amount"`
	CreditedQuota      int64  `json:"credited_quota"`
	UnknownAmountCount int    `json:"unknown_amount_count"`
}
type TopUpDailyTotal struct {
	Date string `json:"date"`
	TopUpCurrencyTotal
}
type TopUpStatistics struct {
	Total      int                  `json:"total"`
	Currencies []TopUpCurrencyTotal `json:"currencies"`
	Daily      []TopUpDailyTotal    `json:"daily"`
	StartTime  int64                `json:"start_time"`
	EndTime    int64                `json:"end_time"`
}

// Totals deliberately never combine different currencies. Unknown legacy
// payment facts remain explicit instead of being silently labelled USD.
func GetTopUpStatistics(filter TopUpFilter) (*TopUpStatistics, error) {
	query, err := topUpFilteredQuery(filter)
	if err != nil {
		return nil, err
	}
	result := &TopUpStatistics{Currencies: []TopUpCurrencyTotal{}, Daily: []TopUpDailyTotal{}, StartTime: filter.StartTime, EndTime: filter.EndTime}
	if result.StartTime == 0 {
		result.StartTime = topUpQueryCutoff()
	}
	currencies := map[string]*TopUpCurrencyTotal{}
	daily := map[string]*TopUpDailyTotal{}
	var batch []TopUp
	err = query.Order("id ASC").FindInBatches(&batch, 500, func(tx *gorm.DB, batchNo int) error {
		for _, row := range batch {
			result.Total++
			currency := row.Currency
			if currencies[currency] == nil {
				currencies[currency] = &TopUpCurrencyTotal{Currency: currency, PaidAmount: "0"}
			}
			date := time.Unix(row.CreateTime, 0).UTC().Format("2006-01-02")
			key := date + ":" + currency
			if daily[key] == nil {
				daily[key] = &TopUpDailyTotal{Date: date, TopUpCurrencyTotal: TopUpCurrencyTotal{Currency: currency, PaidAmount: "0"}}
			}
			for _, total := range []*TopUpCurrencyTotal{currencies[currency], &daily[key].TopUpCurrencyTotal} {
				total.Count++
				if row.Status != common.TopUpStatusSuccess {
					continue
				}
				total.PaidCount++
				total.CreditedQuota += int64(row.CreditedQuota)
				amount, parseErr := decimal.NewFromString(row.PayAmount)
				if currency == "" || parseErr != nil {
					total.UnknownAmountCount++
					continue
				}
				previous, _ := decimal.NewFromString(total.PaidAmount)
				total.PaidAmount = previous.Add(amount).String()
			}
		}
		return nil
	}).Error
	if err != nil {
		return nil, err
	}
	for _, total := range currencies {
		result.Currencies = append(result.Currencies, *total)
	}
	for _, total := range daily {
		result.Daily = append(result.Daily, *total)
	}
	sortTopUpStatistics(result)
	return result, nil
}

func sortTopUpStatistics(result *TopUpStatistics) {
	sort.Slice(result.Currencies, func(i, j int) bool { return result.Currencies[i].Currency < result.Currencies[j].Currency })
	sort.Slice(result.Daily, func(i, j int) bool {
		if result.Daily[i].Date == result.Daily[j].Date {
			return result.Daily[i].Currency < result.Daily[j].Currency
		}
		return result.Daily[i].Date < result.Daily[j].Date
	})
}
