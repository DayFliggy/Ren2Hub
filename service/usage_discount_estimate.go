package service

import (
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// UsageDiscountEstimate replays a draft configuration over recent hourly data.
// It is an approximation, not a bill: amounts are what users were actually
// charged, which is already discounted once a discount is live.
type UsageDiscountEstimate struct {
	FromDate           string `json:"from_date"`
	ToDate             string `json:"to_date"`
	Days               int    `json:"days"`
	RevenueQuota       int64  `json:"revenue_quota"`
	DiscountQuota      int64  `json:"discount_quota"`
	DiscountShare      string `json:"discount_share"`
	CappedRevenueShare string `json:"capped_revenue_share"`
	EarliestHour       *int64 `json:"earliest_hour_utc"`
	HistoryComplete    bool   `json:"history_complete"`
}

// EstimateUsageDiscount evaluates the draft with shadow components treated as
// on, for each of the last usageDiscountEstimateDays complete business days.
func EstimateUsageDiscount(data []byte, now time.Time) (*UsageDiscountEstimate, error) {
	settings, err := model.ValidateUsageDiscountDraft(data, now)
	if err != nil {
		return nil, err
	}
	settings = promoteUsageDiscountShadow(settings)
	local := now.In(settings.Location)
	lookback := usageDiscountEstimateDays + usageDiscountMonthDays
	// daily[i] holds per-user net quota of business day today-lookback+i.
	daily := make([]map[int]int64, lookback)
	dates := make([]string, lookback)
	for i := range daily {
		start := time.Date(local.Year(), local.Month(), local.Day()-lookback+i, 0, 0, 0, 0, settings.Location)
		end := time.Date(local.Year(), local.Month(), local.Day()-lookback+i+1, 0, 0, 0, 0, settings.Location)
		if start.Unix()%3600 != 0 || end.Unix()%3600 != 0 {
			return nil, errUsageDiscountUnalignedDay
		}
		rows, err := model.SumUsageDiscountByUser(start.Unix(), end.Unix())
		if err != nil {
			return nil, err
		}
		daily[i], dates[i] = make(map[int]int64, len(rows)), start.Format(usageDiscountDateLayout)
		for _, row := range rows {
			if !settings.IsExcluded(row.UserID) {
				daily[i][row.UserID] = row.Quota
			}
		}
	}

	revenue, discount, capped := decimal.Zero, decimal.Zero, decimal.Zero
	for i := usageDiscountMonthDays; i < lookback; i++ {
		month := sumUsageDiscountDays(daily[i-usageDiscountMonthDays : i])
		week := sumUsageDiscountDays(daily[i-7 : i])
		var site int64
		for _, quota := range month {
			site += max(quota, 0)
		}
		for userID, spend := range daily[i] {
			if spend <= 0 {
				continue
			}
			decision := evaluateUsageDiscount(settings, &usageDiscountAmounts{
				Site:  site,
				Day:   max(daily[i-1][userID], 0),
				Week:  max(week[userID], 0),
				Month: max(month[userID], 0),
			})
			amount := decimal.NewFromInt(spend)
			revenue = revenue.Add(amount)
			discount = discount.Add(amount.Mul(decision.AppliedRate))
			if decision.CapApplied {
				capped = capped.Add(amount)
			}
		}
	}

	estimate := &UsageDiscountEstimate{
		FromDate:           dates[usageDiscountMonthDays],
		ToDate:             dates[lookback-1],
		Days:               usageDiscountEstimateDays,
		RevenueQuota:       revenue.IntPart(),
		DiscountQuota:      discount.Round(0).IntPart(),
		DiscountShare:      "0",
		CappedRevenueShare: "0",
	}
	if revenue.IsPositive() {
		estimate.DiscountShare = discount.Div(revenue).StringFixed(4)
		estimate.CappedRevenueShare = capped.Div(revenue).StringFixed(4)
	}
	earliest, ok, err := model.EarliestUsageDiscountHour()
	if err != nil {
		return nil, err
	}
	if ok {
		estimate.EarliestHour = &earliest
		lookbackStart := time.Date(local.Year(), local.Month(), local.Day()-lookback, 0, 0, 0, 0, settings.Location).Unix()
		estimate.HistoryComplete = earliest <= lookbackStart
	}
	return estimate, nil
}

func sumUsageDiscountDays(days []map[int]int64) map[int]int64 {
	total := make(map[int]int64)
	for _, day := range days {
		for userID, quota := range day {
			total[userID] += quota
		}
	}
	return total
}

func promoteUsageDiscountShadow(settings *model.UsageDiscountSettings) *model.UsageDiscountSettings {
	promoted := *settings
	for _, table := range []*model.UsageDiscountTable{&promoted.Site, &promoted.Day, &promoted.Week, &promoted.Month} {
		if table.Mode == model.UsageDiscountModeShadow {
			table.Mode = model.UsageDiscountModeOn
		}
	}
	return &promoted
}
