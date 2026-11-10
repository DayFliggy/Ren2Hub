package service

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// TopUpDiscountEstimate replays a draft configuration over recent successful
// top-ups of the discountable providers. It is a quota-equivalent estimate,
// not a bill: discount_quota is the sum of ExpectedQuota × rate.
type TopUpDiscountEstimate struct {
	FromDate           string `json:"from_date"`
	ToDate             string `json:"to_date"`
	Days               int    `json:"days"`
	Orders             int    `json:"orders"`
	UnratedOrders      int    `json:"unrated_orders"`
	CreditedQuota      int64  `json:"credited_quota"`
	DiscountQuota      int64  `json:"discount_quota"`
	DiscountShare      string `json:"discount_share"`
	CappedQuotaShare   string `json:"capped_quota_share"`
	EarliestHour       *int64 `json:"earliest_hour_utc"`
	HistoryCoversStart bool   `json:"history_covers_start"`
}

// topUpDiscountProviders are the providers whose checkout the discount lowers.
var topUpDiscountProviders = []string{model.PaymentProviderEpay, model.PaymentProviderGmpay, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake}

// EstimateTopUpDiscount evaluates the draft with shadow components treated as
// on. Each order is rated with the windows of its business day and its own
// ExpectedQuota; exclusions keep the fixed and amount components.
func EstimateTopUpDiscount(data []byte, now time.Time) (*TopUpDiscountEstimate, error) {
	settings, err := model.ValidateTopUpDiscountDraft(data, now)
	if err != nil {
		return nil, err
	}
	settings = promoteTopUpDiscountShadow(settings)
	local := now.In(settings.Location)
	lookback := topUpDiscountEstimateDays + topUpDiscountMonthDays
	midnight := func(offset int) time.Time {
		return time.Date(local.Year(), local.Month(), local.Day()-lookback+offset, 0, 0, 0, 0, settings.Location)
	}
	// daily[i] holds per-user net usage of business day i of the lookback.
	// Days are bounded by local midnights, so DST days of 23 or 25 hours
	// keep their own orders and windows.
	daily := make([]map[int]int64, lookback)
	starts := make([]int64, lookback+1)
	for i := range starts {
		starts[i] = midnight(i).Unix()
	}
	for i := range daily {
		start, end := midnight(i), midnight(i+1)
		if start.Unix()%3600 != 0 || end.Unix()%3600 != 0 {
			return nil, errTopUpDiscountUnalignedDay
		}
		rows, err := model.SumTopUpDiscountByUser(start.Unix(), end.Unix())
		if err != nil {
			return nil, err
		}
		daily[i] = make(map[int]int64, len(rows))
		for _, row := range rows {
			daily[i][row.UserID] = row.Quota
		}
	}
	orders, err := model.SuccessfulTopUpsBetween(midnight(topUpDiscountMonthDays).Unix(), midnight(lookback).Unix(), topUpDiscountProviders)
	if err != nil {
		return nil, err
	}

	estimate := &TopUpDiscountEstimate{
		FromDate: midnight(topUpDiscountMonthDays).Format(topUpDiscountDateLayout),
		ToDate:   midnight(lookback - 1).Format(topUpDiscountDateLayout),
		Days:     topUpDiscountEstimateDays, DiscountShare: "0", CappedQuotaShare: "0",
	}
	credited, discount, capped := decimal.Zero, decimal.Zero, decimal.Zero
	for _, order := range orders {
		if order.ExpectedQuota <= 0 {
			estimate.UnratedOrders++
			continue
		}
		// The orders query already bounds create_time to the replayed days.
		i := sort.Search(len(starts), func(k int) bool { return starts[k] > order.CreateTime }) - 1
		if i < topUpDiscountMonthDays || i >= lookback {
			continue
		}
		amounts := topUpDiscountAmounts{Order: int64(order.ExpectedQuota)}
		if !settings.IsExcluded(order.UserId) {
			month := sumTopUpDiscountDays(daily[i-topUpDiscountMonthDays : i])
			week := sumTopUpDiscountDays(daily[i-7 : i])
			var site int64
			for userID, quota := range month {
				if !settings.IsExcluded(userID) {
					site += max(quota, 0)
				}
			}
			amounts.Site = &site
			amounts.Personal = &model.TopUpDiscountWindowSums{DayQuota: daily[i-1][order.UserId], WeekQuota: week[order.UserId], MonthQuota: month[order.UserId]}
		}
		decision := evaluateTopUpDiscount(settings, amounts)
		quota := decimal.NewFromInt(int64(order.ExpectedQuota))
		estimate.Orders++
		credited = credited.Add(quota)
		discount = discount.Add(quota.Mul(decision.AppliedRate))
		if decision.CapApplied {
			capped = capped.Add(quota)
		}
	}
	estimate.CreditedQuota = credited.IntPart()
	estimate.DiscountQuota = discount.Round(0).IntPart()
	if credited.IsPositive() {
		estimate.DiscountShare = discount.Div(credited).StringFixed(4)
		estimate.CappedQuotaShare = capped.Div(credited).StringFixed(4)
	}
	earliest, ok, err := model.EarliestTopUpDiscountHour()
	if err != nil {
		return nil, err
	}
	if ok {
		estimate.EarliestHour = &earliest
		estimate.HistoryCoversStart = earliest <= midnight(0).Unix()
	}
	return estimate, nil
}

func sumTopUpDiscountDays(days []map[int]int64) map[int]int64 {
	total := make(map[int]int64)
	for _, day := range days {
		for userID, quota := range day {
			total[userID] += quota
		}
	}
	return total
}

func promoteTopUpDiscountShadow(settings *model.TopUpDiscountSettings) *model.TopUpDiscountSettings {
	promoted := *settings
	for _, table := range []*model.TopUpDiscountTable{&promoted.Amount, &promoted.Site, &promoted.Day, &promoted.Week, &promoted.Month} {
		if table.Mode == model.TopUpDiscountModeShadow {
			table.Mode = model.TopUpDiscountModeOn
		}
	}
	return &promoted
}
