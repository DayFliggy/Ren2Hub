package service

import (
	"sort"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
)

// TopUpBonusEstimate replays a draft configuration over recent credits of
// every bonus source. It is an estimate, not a bill: each credit's bonus is
// floor(principal × rate), as the credit itself would have received.
type TopUpBonusEstimate struct {
	FromDate             string                     `json:"from_date"`
	ToDate               string                     `json:"to_date"`
	Days                 int                        `json:"days"`
	Records              int                        `json:"records"`
	UnratedRecords       int                        `json:"unrated_records"`
	PrincipalQuota       int64                      `json:"principal_quota"`
	BonusQuota           int64                      `json:"bonus_quota"`
	BonusShare           string                     `json:"bonus_share"`
	CappedPrincipalShare string                     `json:"capped_principal_share"`
	Sources              []TopUpBonusEstimateSource `json:"sources"`
	EarliestHour         *int64                     `json:"earliest_hour_utc"`
	HistoryCoversStart   bool                       `json:"history_covers_start"`
}

// TopUpBonusEstimateSource sums the replayed credits of one source.
type TopUpBonusEstimateSource struct {
	Source         string `json:"source"`
	Records        int    `json:"records"`
	PrincipalQuota int64  `json:"principal_quota"`
	BonusQuota     int64  `json:"bonus_quota"`
}

// topUpBonusProviders are the online providers whose orders carry a bonus.
var topUpBonusProviders = []string{
	model.PaymentProviderEpay, model.PaymentProviderGmpay, model.PaymentProviderStripe,
	model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake,
}

// topUpBonusCredit is one replayed credit; a principal of 0 cannot be rated.
type topUpBonusCredit struct {
	source    int
	userID    int
	at        int64
	principal int64
}

// EstimateTopUpBonus evaluates the draft with shadow components treated as
// on. Each credit is rated with the windows of its business day and its own
// principal; exclusions keep the fixed and amount components.
func EstimateTopUpBonus(data []byte, now time.Time) (*TopUpBonusEstimate, error) {
	settings, err := model.ValidateTopUpBonusDraft(data, now)
	if err != nil {
		return nil, err
	}
	settings = promoteTopUpBonusShadow(settings)
	local := now.In(settings.Location)
	lookback := topUpBonusEstimateDays + topUpBonusMonthDays
	midnight := func(offset int) time.Time {
		return time.Date(local.Year(), local.Month(), local.Day()-lookback+offset, 0, 0, 0, 0, settings.Location)
	}
	// daily[i] holds per-user net usage of business day i of the lookback.
	// Days are bounded by local midnights, so DST days of 23 or 25 hours
	// keep their own credits and windows.
	daily := make([]map[int]int64, lookback)
	starts := make([]int64, lookback+1)
	for i := range starts {
		starts[i] = midnight(i).Unix()
	}
	for i := range daily {
		start, end := midnight(i), midnight(i+1)
		if start.Unix()%3600 != 0 || end.Unix()%3600 != 0 {
			return nil, errTopUpBonusUnalignedDay
		}
		rows, err := model.SumTopUpBonusByUser(start.Unix(), end.Unix())
		if err != nil {
			return nil, err
		}
		daily[i] = make(map[int]int64, len(rows))
		for _, row := range rows {
			daily[i][row.UserID] = row.Quota
		}
	}
	credits, err := topUpBonusCreditsBetween(midnight(topUpBonusMonthDays).Unix(), midnight(lookback).Unix())
	if err != nil {
		return nil, err
	}

	estimate := &TopUpBonusEstimate{
		FromDate: midnight(topUpBonusMonthDays).Format(topUpBonusDateLayout),
		ToDate:   midnight(lookback - 1).Format(topUpBonusDateLayout),
		Days:     topUpBonusEstimateDays, BonusShare: "0", CappedPrincipalShare: "0",
		Sources: []TopUpBonusEstimateSource{
			{Source: model.TopUpBonusSourceTopUp}, {Source: model.TopUpBonusSourceRedemption}, {Source: model.TopUpBonusSourceAdminQuota},
		},
	}
	var capped int64
	for _, credit := range credits {
		if credit.principal <= 0 {
			estimate.UnratedRecords++
			continue
		}
		// The queries already bound each credit's time to the replayed days.
		i := sort.Search(len(starts), func(k int) bool { return starts[k] > credit.at }) - 1
		if i < topUpBonusMonthDays || i >= lookback {
			continue
		}
		principal := credit.principal
		amounts := topUpBonusAmounts{Principal: &principal}
		if !settings.IsExcluded(credit.userID) {
			month := sumTopUpBonusDays(daily[i-topUpBonusMonthDays : i])
			week := sumTopUpBonusDays(daily[i-7 : i])
			var site int64
			for userID, quota := range month {
				if !settings.IsExcluded(userID) {
					site += max(quota, 0)
				}
			}
			amounts.Site = &site
			amounts.Personal = &model.TopUpBonusWindowSums{DayQuota: daily[i-1][credit.userID], WeekQuota: week[credit.userID], MonthQuota: month[credit.userID]}
		}
		decision := evaluateTopUpBonus(settings, amounts)
		source := &estimate.Sources[credit.source]
		source.Records++
		source.PrincipalQuota += principal
		source.BonusQuota += decision.BonusQuota
		if decision.CapApplied {
			capped += principal
		}
	}
	for _, source := range estimate.Sources {
		estimate.Records += source.Records
		estimate.PrincipalQuota += source.PrincipalQuota
		estimate.BonusQuota += source.BonusQuota
	}
	if estimate.PrincipalQuota > 0 {
		principal := decimal.NewFromInt(estimate.PrincipalQuota)
		estimate.BonusShare = decimal.NewFromInt(estimate.BonusQuota).Div(principal).StringFixed(4)
		estimate.CappedPrincipalShare = decimal.NewFromInt(capped).Div(principal).StringFixed(4)
	}
	earliest, ok, err := model.EarliestTopUpBonusHour()
	if err != nil {
		return nil, err
	}
	if ok {
		estimate.EarliestHour = &earliest
		estimate.HistoryCoversStart = earliest <= midnight(0).Unix()
	}
	return estimate, nil
}

// topUpBonusCreditsBetween lists the credits of every source in [start, end),
// indexed like TopUpBonusEstimate.Sources. An order's principal is its full
// credit minus its frozen bonus.
func topUpBonusCreditsBetween(start, end int64) ([]topUpBonusCredit, error) {
	orders, err := model.SuccessfulTopUpsBetween(start, end, topUpBonusProviders)
	if err != nil {
		return nil, err
	}
	codes, err := model.RedeemedCodesBetween(start, end)
	if err != nil {
		return nil, err
	}
	additions, err := model.AdminQuotaAddsBetween(start, end)
	if err != nil {
		return nil, err
	}
	credits := make([]topUpBonusCredit, 0, len(orders)+len(codes)+len(additions))
	for _, order := range orders {
		principal := max(int64(order.ExpectedQuota-order.BonusQuota), 0)
		credits = append(credits, topUpBonusCredit{source: 0, userID: order.UserId, at: order.CreateTime, principal: principal})
	}
	for _, code := range codes {
		credits = append(credits, topUpBonusCredit{source: 1, userID: code.UsedUserId, at: code.RedeemedTime, principal: int64(code.Quota)})
	}
	for _, addition := range additions {
		credits = append(credits, topUpBonusCredit{source: 2, userID: addition.UserID, at: addition.CreatedAt, principal: int64(addition.Value)})
	}
	return credits, nil
}

func sumTopUpBonusDays(days []map[int]int64) map[int]int64 {
	total := make(map[int]int64)
	for _, day := range days {
		for userID, quota := range day {
			total[userID] += quota
		}
	}
	return total
}

func promoteTopUpBonusShadow(settings *model.TopUpBonusSettings) *model.TopUpBonusSettings {
	promoted := *settings
	for _, table := range []*model.TopUpBonusTable{&promoted.Amount, &promoted.Site, &promoted.Day, &promoted.Week, &promoted.Month} {
		if table.Mode == model.TopUpBonusModeShadow {
			table.Mode = model.TopUpBonusModeOn
		}
	}
	return &promoted
}
