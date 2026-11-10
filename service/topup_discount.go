package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
)

// Windows are summed from hourly rows strictly before the business-day cutoff
// and cached for the day. Those rows usually stop changing at the cutoff; the
// exceptions are listed in docs/billing/TOPUP_DISCOUNT.md.

const (
	topUpDiscountMonthDays      = 30
	topUpDiscountRetentionDays  = 90
	topUpDiscountEstimateDays   = 30
	topUpDiscountDateLayout     = time.DateOnly
	topUpDiscountWindowDay      = "day"
	topUpDiscountWindowWeek     = "week"
	topUpDiscountWindowMonth    = "month"
	topUpDiscountReasonExcluded = "excluded_user"
	// A failing site query is retried at most this often: requests waiting on
	// the site mutex would otherwise each pay the full statement timeout.
	topUpDiscountSiteRetryAfter = 10 * time.Second
)

var errTopUpDiscountUnalignedDay = errors.New("business day boundary is not a whole UTC hour")

// topUpDiscountDay holds one business day's boundaries and cached sums.
type topUpDiscountDay struct {
	date       string
	timezone   string
	dayStart   int64
	weekStart  int64
	monthStart int64
	cutoff     int64

	mu    sync.Mutex
	users map[int]model.TopUpDiscountWindowSums
	// loads merges concurrent first reads of one user into one query, so every
	// request of the day sees the same sums.
	loads singleflight.Group

	siteMu       sync.Mutex
	siteVersion  int64
	siteReady    bool
	siteQuota    int64
	siteErr      error
	siteFailedAt time.Time
}

var currentTopUpDiscountDay atomic.Pointer[topUpDiscountDay]

// ResetTopUpDiscountCaches drops cached settings and window sums, for callers
// that replace the database.
func ResetTopUpDiscountCaches() {
	model.ResetTopUpDiscountSettingsCache()
	currentTopUpDiscountDay.Store(nil)
}

func newTopUpDiscountDay(location *time.Location, now time.Time) (*topUpDiscountDay, error) {
	local := now.In(location)
	midnight := func(daysBack int) int64 {
		return time.Date(local.Year(), local.Month(), local.Day()-daysBack, 0, 0, 0, 0, location).Unix()
	}
	day := &topUpDiscountDay{
		date:       local.Format(topUpDiscountDateLayout),
		timezone:   location.String(),
		cutoff:     midnight(0),
		dayStart:   midnight(1),
		weekStart:  midnight(7),
		monthStart: midnight(topUpDiscountMonthDays),
		users:      make(map[int]model.TopUpDiscountWindowSums),
	}
	for _, boundary := range []int64{day.cutoff, day.dayStart, day.weekStart, day.monthStart} {
		if boundary%3600 != 0 {
			return nil, fmt.Errorf("%w: %s on %s", errTopUpDiscountUnalignedDay, day.timezone, day.date)
		}
	}
	return day, nil
}

// topUpDiscountDayFor returns the cache for now's business day, replacing an
// older one. Replacing also prunes hourly rows past the retention window.
func topUpDiscountDayFor(location *time.Location, now time.Time) (*topUpDiscountDay, error) {
	next, err := newTopUpDiscountDay(location, now)
	if err != nil {
		return nil, err
	}
	for {
		current := currentTopUpDiscountDay.Load()
		if current != nil && current.timezone == next.timezone {
			if current.date == next.date {
				return current, nil
			}
			if current.date > next.date {
				// A request that read the clock just before midnight; serve it
				// without moving the shared cache back a day.
				return next, nil
			}
		}
		if currentTopUpDiscountDay.CompareAndSwap(current, next) {
			pruneTopUpDiscountUsageHourly(next)
			return next, nil
		}
	}
}

func pruneTopUpDiscountUsageHourly(day *topUpDiscountDay) {
	before := day.cutoff - int64(topUpDiscountRetentionDays)*24*3600
	if err := model.DeleteTopUpDiscountUsageHourlyBefore(before); err != nil {
		common.SysError(common.LogText("failed to prune top-up discount usage rows: %v", err))
	}
}

func (d *topUpDiscountDay) windowSums(userID int) (model.TopUpDiscountWindowSums, error) {
	if sums, ok := d.cachedSums(userID); ok {
		return sums, nil
	}
	loaded, err, _ := d.loads.Do(strconv.Itoa(userID), func() (any, error) {
		// A load that finished just before this one joined already cached it.
		if sums, ok := d.cachedSums(userID); ok {
			return sums, nil
		}
		sums, err := model.SumTopUpDiscountWindows(userID, d.dayStart, d.weekStart, d.monthStart, d.cutoff)
		if err != nil {
			return sums, err
		}
		d.mu.Lock()
		d.users[userID] = sums
		d.mu.Unlock()
		return sums, nil
	})
	return loaded.(model.TopUpDiscountWindowSums), err
}

func (d *topUpDiscountDay) cachedSums(userID int) (model.TopUpDiscountWindowSums, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sums, ok := d.users[userID]
	return sums, ok
}

func (d *topUpDiscountDay) cachedUsers() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.users)
}

// siteMonthQuota is the 30-day total of users outside the exclusion list, each
// floored at 0. It is cached per config version because exclusions may change.
func (d *topUpDiscountDay) siteMonthQuota(settings *model.TopUpDiscountSettings, now time.Time) (int64, error) {
	d.siteMu.Lock()
	defer d.siteMu.Unlock()
	if d.siteReady && d.siteVersion == settings.Version {
		return d.siteQuota, nil
	}
	if d.siteErr != nil && now.Sub(d.siteFailedAt) < topUpDiscountSiteRetryAfter {
		return 0, d.siteErr
	}
	rows, err := model.SumTopUpDiscountByUser(d.monthStart, d.cutoff)
	if err != nil {
		d.siteErr, d.siteFailedAt = err, now
		return 0, err
	}
	d.siteErr = nil
	var total int64
	for _, row := range rows {
		if !settings.IsExcluded(row.UserID) {
			total += max(row.Quota, 0)
		}
	}
	d.siteReady, d.siteVersion, d.siteQuota = true, settings.Version, total
	return total, nil
}

// topUpDiscountAmounts are the inputs of one evaluation. A nil pointer means
// the value was not read (component off, user excluded or the query failed);
// it is never treated as zero usage.
type topUpDiscountAmounts struct {
	Order    int64
	Site     *int64
	Personal *model.TopUpDiscountWindowSums
}

// evaluateTopUpDiscount applies the formula once:
// min(amount + fixed + site + max(day, week, month), cap).
func evaluateTopUpDiscount(settings *model.TopUpDiscountSettings, amounts topUpDiscountAmounts) *TopUpDiscountDecision {
	decision := &TopUpDiscountDecision{
		Status:        TopUpDiscountOK,
		FixedRate:     settings.FixedRate,
		CapRate:       settings.CapRate,
		ConfigVersion: settings.Version,
	}
	decision.AmountRate = topUpDiscountComponentRate(decision, "amount", settings.Amount, &amounts.Order)
	decision.SiteRate = topUpDiscountComponentRate(decision, "site", settings.Site, amounts.Site)
	var day, week, month *int64
	if amounts.Personal != nil {
		day, week, month = &amounts.Personal.DayQuota, &amounts.Personal.WeekQuota, &amounts.Personal.MonthQuota
	}
	decision.DayRate = topUpDiscountComponentRate(decision, topUpDiscountWindowDay, settings.Day, day)
	decision.WeekRate = topUpDiscountComponentRate(decision, topUpDiscountWindowWeek, settings.Week, week)
	decision.MonthRate = topUpDiscountComponentRate(decision, topUpDiscountWindowMonth, settings.Month, month)
	userRate := decimal.Zero
	// Ties go to the longer window, which is the more stable explanation.
	for _, window := range []struct {
		name string
		rate decimal.Decimal
	}{{topUpDiscountWindowMonth, decision.MonthRate}, {topUpDiscountWindowWeek, decision.WeekRate}, {topUpDiscountWindowDay, decision.DayRate}} {
		if window.rate.GreaterThan(userRate) {
			userRate, decision.WinnerWindow = window.rate, window.name
		}
	}
	decision.RawRate = decision.AmountRate.Add(decision.FixedRate).Add(decision.SiteRate).Add(userRate)
	decision.AppliedRate = decimal.Min(decision.RawRate, settings.CapRate)
	decision.CapApplied = decision.RawRate.GreaterThan(settings.CapRate)
	decision.Factor = decimal.NewFromInt(1).Sub(decision.AppliedRate)
	return decision
}

// topUpDiscountComponentRate returns the rate that takes effect. A shadow
// component is recorded for administrators and contributes 0; an unread
// amount contributes 0 (the caller has already rejected unread "on" tables).
func topUpDiscountComponentRate(decision *TopUpDiscountDecision, name string, table model.TopUpDiscountTable, amount *int64) decimal.Decimal {
	if table.Mode == model.TopUpDiscountModeOff || amount == nil {
		return decimal.Zero
	}
	rate := topUpDiscountTierRate(table, max(*amount, 0))
	if table.Mode == model.TopUpDiscountModeShadow {
		if decision.ShadowRates == nil {
			decision.ShadowRates = make(map[string]string)
		}
		decision.ShadowRates[name] = rate.String()
		return decimal.Zero
	}
	return rate
}

// topUpDiscountTierRate is the rate of the highest tier whose threshold the
// amount reaches; below the first tier it is 0.
func topUpDiscountTierRate(table model.TopUpDiscountTable, amount int64) decimal.Decimal {
	rate := decimal.Zero
	for _, tier := range table.Tiers {
		if amount < tier.Threshold {
			break
		}
		rate = tier.Rate
	}
	return rate
}

type topUpDiscountResolution struct {
	Decision *TopUpDiscountDecision
	Personal *model.TopUpDiscountWindowSums
}

// resolveTopUpDiscount decides the discount of one top-up of orderQuota.
// It fails only when a table that changes the price cannot be read; a failed
// shadow-only source is recorded and the other components still apply.
func resolveTopUpDiscount(settings *model.TopUpDiscountSettings, userID int, orderQuota int64, now time.Time) (topUpDiscountResolution, error) {
	if !settings.Enabled() {
		decision := neutralTopUpDiscount(TopUpDiscountInactive)
		decision.ConfigVersion = settings.Version
		return topUpDiscountResolution{Decision: decision}, nil
	}
	amounts := topUpDiscountAmounts{Order: orderQuota}
	excluded := settings.IsExcluded(userID)
	var shadowFailures []string
	var businessDate string
	if !excluded && (settings.UsesSite() || settings.UsesWindows()) {
		day, err := topUpDiscountDayFor(settings.Location, now)
		if err != nil {
			return topUpDiscountResolution{}, fmt.Errorf("%w: %v", ErrTopUpDiscountUnavailable, err)
		}
		businessDate = day.date
		if settings.UsesSite() {
			site, err := day.siteMonthQuota(settings, now)
			switch {
			case err == nil:
				amounts.Site = &site
			case settings.Site.Mode == model.TopUpDiscountModeOn:
				common.SysError(common.LogText("top-up discount site total unavailable: %v", err))
				return topUpDiscountResolution{}, fmt.Errorf("%w: %v", ErrTopUpDiscountUnavailable, err)
			default:
				shadowFailures = append(shadowFailures, "site")
			}
		}
		if settings.UsesWindows() {
			sums, err := day.windowSums(userID)
			switch {
			case err == nil:
				amounts.Personal = &sums
			case settings.PersonalOn():
				common.SysError(common.LogText("top-up discount windows unavailable: user_id=%d, error=%v", userID, err))
				return topUpDiscountResolution{}, fmt.Errorf("%w: %v", ErrTopUpDiscountUnavailable, err)
			default:
				shadowFailures = append(shadowFailures, "personal")
			}
		}
	}
	decision := evaluateTopUpDiscount(settings, amounts)
	decision.BusinessDate = businessDate
	switch {
	case excluded:
		decision.Reason = topUpDiscountReasonExcluded
	case len(shadowFailures) > 0:
		decision.Status, decision.Reason = TopUpDiscountShadowUnavailable, strings.Join(shadowFailures, ",")
	}
	return topUpDiscountResolution{Decision: decision, Personal: amounts.Personal}, nil
}

// TopUpDiscountSelfView is what a user may see about the discount of their
// next top-up: no site total, no exclusion list and no shadow components.
// The amount tier depends on the order, so it is listed rather than applied.
type TopUpDiscountSelfView struct {
	Status       string                          `json:"status"`
	Factor       string                          `json:"factor"`
	FixedRate    string                          `json:"fixed_rate"`
	SiteRate     string                          `json:"site_rate"`
	PersonalRate string                          `json:"personal_rate"`
	RawRate      string                          `json:"raw_rate"`
	AppliedRate  string                          `json:"applied_rate"`
	CapRate      string                          `json:"cap_rate"`
	CapApplied   bool                            `json:"cap_applied"`
	WinnerWindow string                          `json:"winner_window,omitempty"`
	BusinessDate string                          `json:"business_date,omitempty"`
	AmountTiers  []model.TopUpDiscountTierConfig `json:"amount_tiers"`
	Windows      []TopUpDiscountWindowView       `json:"windows"`
}

type TopUpDiscountWindowView struct {
	Window string                          `json:"window"`
	Quota  int64                           `json:"quota"`
	Rate   string                          `json:"rate"`
	Tiers  []model.TopUpDiscountTierConfig `json:"tiers"`
}

func GetTopUpDiscountSelfView(userID int, now time.Time) (*TopUpDiscountSelfView, error) {
	settings, err := model.CurrentTopUpDiscountSettings()
	if err != nil {
		return nil, err
	}
	return topUpDiscountSelfView(settings, userID, now)
}

// topUpDiscountSelfView evaluates every component except the order-dependent
// amount tier, which is listed in AmountTiers instead, even a 0 threshold.
func topUpDiscountSelfView(settings *model.TopUpDiscountSettings, userID int, now time.Time) (*TopUpDiscountSelfView, error) {
	view := &TopUpDiscountSelfView{
		Status: TopUpDiscountInactive, Factor: "1", FixedRate: "0", SiteRate: "0", PersonalRate: "0", RawRate: "0", AppliedRate: "0",
		CapRate: settings.CapRate.String(), AmountTiers: []model.TopUpDiscountTierConfig{}, Windows: []TopUpDiscountWindowView{},
	}
	if !settings.Active() {
		// Shadow-only configurations change nothing for the user.
		return view, nil
	}
	withoutAmount := *settings
	withoutAmount.Amount = model.TopUpDiscountTable{Mode: model.TopUpDiscountModeOff}
	result, err := resolveTopUpDiscount(&withoutAmount, userID, 0, now)
	if err != nil {
		return nil, err
	}
	decision := result.Decision
	view.Status = decision.Status
	if view.Status == TopUpDiscountShadowUnavailable {
		view.Status = TopUpDiscountOK
	}
	view.Factor, view.FixedRate, view.SiteRate = decision.Factor.String(), decision.FixedRate.String(), decision.SiteRate.String()
	view.PersonalRate = decimal.Max(decision.DayRate, decision.WeekRate, decision.MonthRate).String()
	view.RawRate, view.AppliedRate, view.CapApplied = decision.RawRate.String(), decision.AppliedRate.String(), decision.CapApplied
	view.WinnerWindow, view.BusinessDate = decision.WinnerWindow, decision.BusinessDate
	if settings.Amount.Mode == model.TopUpDiscountModeOn {
		view.AmountTiers = settings.Config.Amount.Tiers
	}
	if result.Personal == nil {
		return view, nil
	}
	for _, window := range []struct {
		name   string
		config model.TopUpDiscountTableConfig
		quota  int64
		rate   decimal.Decimal
	}{
		{topUpDiscountWindowDay, settings.Config.Personal.Day, result.Personal.DayQuota, decision.DayRate},
		{topUpDiscountWindowWeek, settings.Config.Personal.Week, result.Personal.WeekQuota, decision.WeekRate},
		{topUpDiscountWindowMonth, settings.Config.Personal.Month, result.Personal.MonthQuota, decision.MonthRate},
	} {
		if window.config.Mode == model.TopUpDiscountModeOn {
			view.Windows = append(view.Windows, TopUpDiscountWindowView{
				Window: window.name, Quota: max(window.quota, 0), Rate: window.rate.String(), Tiers: window.config.Tiers,
			})
		}
	}
	return view, nil
}

// TopUpDiscountStatusView is the Root health view.
type TopUpDiscountStatusView struct {
	ConfigVersion  int64                     `json:"config_version"`
	Timezone       string                    `json:"timezone"`
	BusinessDate   string                    `json:"business_date"`
	CutoffUTC      int64                     `json:"cutoff_utc"`
	CachedUsers    int                       `json:"cached_users"`
	SiteMonthQuota int64                     `json:"site_month_quota"`
	SiteTierRate   string                    `json:"site_tier_rate"`
	EarliestHour   *int64                    `json:"earliest_hour_utc"`
	Health         model.TopUpDiscountHealth `json:"health"`
}

func GetTopUpDiscountStatus(now time.Time) (*TopUpDiscountStatusView, error) {
	settings, err := model.CurrentTopUpDiscountSettings()
	if err != nil {
		return nil, err
	}
	day, err := topUpDiscountDayFor(settings.Location, now)
	if err != nil {
		return nil, err
	}
	siteQuota, err := day.siteMonthQuota(settings, now)
	if err != nil {
		return nil, err
	}
	view := &TopUpDiscountStatusView{
		ConfigVersion:  settings.Version,
		Timezone:       day.timezone,
		BusinessDate:   day.date,
		CutoffUTC:      day.cutoff,
		CachedUsers:    day.cachedUsers(),
		SiteMonthQuota: siteQuota,
		SiteTierRate:   topUpDiscountTierRate(settings.Site, siteQuota).String(),
		Health:         model.GetTopUpDiscountHealth(),
	}
	earliest, ok, err := model.EarliestTopUpDiscountHour()
	if err != nil {
		return nil, err
	}
	if ok {
		view.EarliestHour = &earliest
	}
	return view, nil
}
