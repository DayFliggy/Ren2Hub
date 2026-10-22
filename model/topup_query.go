package model

import (
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type TopUpFilter struct {
	UserID int
	// SelfView 表示普通用户查看自己的订单：起始时间不早于 30 天截止点。
	// 管理员查询（包括带 user_id 的查询）与上游 GetAllTopUps 一致，没有默认下限。
	SelfView                          bool
	Keyword, Status, Method, Currency string
	StartTime, EndTime                int64
}

// TopUpMethodOther 汇总未知或缺失的支付方式。
const TopUpMethodOther = "other"

// topUpKnownMethods 是订单筛选和统计共用的已知支付方式集合。
var topUpKnownMethods = []string{
	PaymentProviderEpay, PaymentProviderGmpay, PaymentProviderStripe, PaymentProviderCreem,
	PaymentProviderWaffo, PaymentProviderWaffoPancake, PaymentProviderBalance,
}

// topUpEffectiveMethodSQL 与 topUpEffectiveMethod 表达同一规则：payment_provider 非空时取它，
// 否则取 payment_method；两列都可能是 NULL 或空串。
const topUpEffectiveMethodSQL = "COALESCE(NULLIF(payment_provider, ''), payment_method, '')"

// topUpEffectiveMethod 返回订单的有效支付方式，不在已知集合内的归为 other。
// 不按 payment_method 推断渠道：没有 provider 的遗留订单保持 other，不猜成 epay。
func topUpEffectiveMethod(provider, method string) string {
	effective := provider
	if effective == "" {
		effective = method
	}
	if slices.Contains(topUpKnownMethods, effective) {
		return effective
	}
	return TopUpMethodOther
}

// topUpMethodScope 按有效支付方式筛选；只接受已知方式和 other。
func topUpMethodScope(query *gorm.DB, method string) (*gorm.DB, error) {
	if method == TopUpMethodOther {
		return query.Where(topUpEffectiveMethodSQL+" NOT IN ?", topUpKnownMethods), nil
	}
	if !slices.Contains(topUpKnownMethods, method) {
		return nil, errors.New("invalid payment method")
	}
	return query.Where(topUpEffectiveMethodSQL+" = ?", method), nil
}

// topUpQueryStart 返回生效的起始时间，0 表示不限制。
func topUpQueryStart(filter TopUpFilter) int64 {
	if !filter.SelfView {
		return filter.StartTime
	}
	return max(filter.StartTime, topUpQueryCutoff())
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
		pattern, err := sanitizeLikePattern(filter.Keyword)
		if err != nil {
			return nil, err
		}
		query = query.Where("trade_no LIKE ? ESCAPE '!'", pattern)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Method != "" {
		var err error
		if query, err = topUpMethodScope(query, filter.Method); err != nil {
			return nil, err
		}
	}
	if filter.Currency != "" {
		query = query.Where("currency = ?", strings.ToUpper(filter.Currency))
	}
	start := topUpQueryStart(filter)
	if start > 0 {
		query = query.Where("create_time >= ?", start)
	}
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
	// 计数最多 searchTopUpCountHardLimit 行，避免对大表做无界 COUNT。
	// GORM 的 Limit(n).Count() 不限制计数，所以用子查询封顶。
	var total int64
	capped := query.Session(&gorm.Session{}).Select("id").Limit(searchTopUpCountHardLimit)
	if err = DB.Table("(?) AS capped_top_ups", capped).Count(&total).Error; err != nil {
		common.SysError(common.LogText("failed to count topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}
	var rows []TopUp
	if err = query.Order("id DESC").Offset(page.GetStartIdx()).Limit(page.GetPageSize()).Find(&rows).Error; err != nil {
		common.SysError(common.LogText("failed to search topups: %s", err.Error()))
		return nil, 0, common.NewMessage("Failed to search top-up records")
	}
	return rows, total, nil
}

type TopUpDetail struct {
	*TopUp
	Username    string      `json:"username"`
	Email       string      `json:"email"`
	UserDeleted bool        `json:"user_deleted"`
	Gmpay       *GmpayOrder `json:"gmpay"`
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
	// Orders outlive their account; a deleted owner keeps the stored user_id.
	user, err := GetOwnerIncludingDeleted(row.UserId)
	if err != nil {
		return nil, err
	}
	result.UserDeleted = OwnerDeleted(user)
	if user != nil {
		result.Username, result.Email = user.Username, user.Email
	}
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

// TopUpMethodTotal 按有效支付方式（见 topUpEffectiveMethod）和币种汇总。
type TopUpMethodTotal struct {
	Method string `json:"method"`
	TopUpCurrencyTotal
}
type TopUpStatistics struct {
	Total      int                  `json:"total"`
	Currencies []TopUpCurrencyTotal `json:"currencies"`
	Methods    []TopUpMethodTotal   `json:"methods"`
	Daily      []TopUpDailyTotal    `json:"daily"`
	StartTime  int64                `json:"start_time"`
	EndTime    int64                `json:"end_time"`
}

// Totals deliberately never combine different currencies. Unknown legacy
// payment facts remain explicit instead of being silently labelled USD.
// Daily totals use the viewer's calendar days in loc.
func GetTopUpStatistics(filter TopUpFilter, loc *time.Location) (*TopUpStatistics, error) {
	query, err := topUpFilteredQuery(filter)
	if err != nil {
		return nil, err
	}
	result := &TopUpStatistics{Currencies: []TopUpCurrencyTotal{}, Methods: []TopUpMethodTotal{}, Daily: []TopUpDailyTotal{}, StartTime: topUpQueryStart(filter), EndTime: filter.EndTime}
	currencies := map[string]*TopUpCurrencyTotal{}
	methods := map[string]*TopUpMethodTotal{}
	daily := map[string]*TopUpDailyTotal{}
	var batch []TopUp
	err = query.Order("id ASC").FindInBatches(&batch, 500, func(tx *gorm.DB, batchNo int) error {
		for _, row := range batch {
			result.Total++
			currency := row.Currency
			if currencies[currency] == nil {
				currencies[currency] = &TopUpCurrencyTotal{Currency: currency, PaidAmount: "0"}
			}
			date := time.Unix(row.CreateTime, 0).In(loc).Format("2006-01-02")
			key := date + ":" + currency
			if daily[key] == nil {
				daily[key] = &TopUpDailyTotal{Date: date, TopUpCurrencyTotal: TopUpCurrencyTotal{Currency: currency, PaidAmount: "0"}}
			}
			method := topUpEffectiveMethod(row.PaymentProvider, row.PaymentMethod)
			methodKey := method + ":" + currency
			if methods[methodKey] == nil {
				methods[methodKey] = &TopUpMethodTotal{Method: method, TopUpCurrencyTotal: TopUpCurrencyTotal{Currency: currency, PaidAmount: "0"}}
			}
			for _, total := range []*TopUpCurrencyTotal{currencies[currency], &methods[methodKey].TopUpCurrencyTotal, &daily[key].TopUpCurrencyTotal} {
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
	for _, total := range methods {
		result.Methods = append(result.Methods, *total)
	}
	for _, total := range daily {
		result.Daily = append(result.Daily, *total)
	}
	sortTopUpStatistics(result)
	return result, nil
}

func sortTopUpStatistics(result *TopUpStatistics) {
	sort.Slice(result.Currencies, func(i, j int) bool { return result.Currencies[i].Currency < result.Currencies[j].Currency })
	sort.Slice(result.Methods, func(i, j int) bool {
		if result.Methods[i].Method == result.Methods[j].Method {
			return result.Methods[i].Currency < result.Methods[j].Currency
		}
		return result.Methods[i].Method < result.Methods[j].Method
	})
	sort.Slice(result.Daily, func(i, j int) bool {
		if result.Daily[i].Date == result.Daily[j].Date {
			return result.Daily[i].Currency < result.Daily[j].Currency
		}
		return result.Daily[i].Date < result.Daily[j].Date
	})
}

const (
	TopUpRankingDefaultLimit = 10
	TopUpRankingMaxLimit     = 50
)

// TopUpRankingEntry 只包含用户标识和到账汇总，不含支付凭据或订单明细。
type TopUpRankingEntry struct {
	UserID        int    `json:"user_id"`
	Username      string `json:"username"`
	PaidCount     int64  `json:"paid_count"`
	CreditedQuota int64  `json:"credited_quota"`
}
type TopUpRanking struct {
	Items     []TopUpRankingEntry `json:"items"`
	StartTime int64               `json:"start_time"`
	EndTime   int64               `json:"end_time"`
}

// GetTopUpRanking 按到账额度从高到低排列用户，同额按 user_id 升序；只统计成功订单，其余条件与订单列表相同。
func GetTopUpRanking(filter TopUpFilter, limit int) (*TopUpRanking, error) {
	if limit < 1 || limit > TopUpRankingMaxLimit {
		return nil, errors.New("invalid ranking limit")
	}
	query, err := topUpFilteredQuery(filter)
	if err != nil {
		return nil, err
	}
	result := &TopUpRanking{Items: []TopUpRankingEntry{}, StartTime: topUpQueryStart(filter), EndTime: filter.EndTime}
	err = query.Where("status = ?", common.TopUpStatusSuccess).
		Select("user_id, COUNT(*) AS paid_count, SUM(credited_quota) AS credited_quota").
		Group("user_id").Order("SUM(credited_quota) DESC, user_id ASC").Limit(limit).
		Scan(&result.Items).Error
	if err != nil || len(result.Items) == 0 {
		return result, err
	}
	ids := make([]int, len(result.Items))
	for i, item := range result.Items {
		ids[i] = item.UserID
	}
	var users []User
	if err = DB.Unscoped().Select("id", "username").Where("id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	names := make(map[int]string, len(users))
	for _, user := range users {
		names[user.Id] = user.Username
	}
	for i := range result.Items {
		result.Items[i].Username = names[result.Items[i].UserID]
	}
	return result, nil
}
