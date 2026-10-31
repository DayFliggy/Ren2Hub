package service

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/singleflight"
)

// Windows are summed from hourly rows strictly before the business-day cutoff
// and cached for the day. Those rows usually stop changing at the cutoff; the
// exceptions are listed in docs/billing/USAGE_DISCOUNT.md.

const (
	usageDiscountMonthDays      = 30
	usageDiscountRetentionDays  = 90
	usageDiscountEstimateDays   = 30
	usageDiscountDateLayout     = time.DateOnly
	usageDiscountWindowDay      = "day"
	usageDiscountWindowWeek     = "week"
	usageDiscountWindowMonth    = "month"
	usageDiscountReasonExcluded = "excluded_user"
	// A failing site query is retried at most this often: requests waiting on
	// the site mutex would otherwise each pay the full statement timeout.
	usageDiscountSiteRetryAfter = 10 * time.Second
)

var errUsageDiscountUnalignedDay = errors.New("business day boundary is not a whole UTC hour")

// usageDiscountDay holds one business day's boundaries and cached sums.
type usageDiscountDay struct {
	date       string
	timezone   string
	dayStart   int64
	weekStart  int64
	monthStart int64
	cutoff     int64

	mu    sync.Mutex
	users map[int]model.UsageDiscountWindowSums
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

var currentUsageDiscountDay atomic.Pointer[usageDiscountDay]

// ResetUsageDiscountCaches drops cached settings and window sums, for callers
// that replace the database.
func ResetUsageDiscountCaches() {
	model.ResetUsageDiscountSettingsCache()
	currentUsageDiscountDay.Store(nil)
}

func newUsageDiscountDay(location *time.Location, now time.Time) (*usageDiscountDay, error) {
	local := now.In(location)
	midnight := func(daysBack int) int64 {
		return time.Date(local.Year(), local.Month(), local.Day()-daysBack, 0, 0, 0, 0, location).Unix()
	}
	day := &usageDiscountDay{
		date:       local.Format(usageDiscountDateLayout),
		timezone:   location.String(),
		cutoff:     midnight(0),
		dayStart:   midnight(1),
		weekStart:  midnight(7),
		monthStart: midnight(usageDiscountMonthDays),
		users:      make(map[int]model.UsageDiscountWindowSums),
	}
	for _, boundary := range []int64{day.cutoff, day.dayStart, day.weekStart, day.monthStart} {
		if boundary%3600 != 0 {
			return nil, fmt.Errorf("%w: %s on %s", errUsageDiscountUnalignedDay, day.timezone, day.date)
		}
	}
	return day, nil
}

// usageDiscountDayFor returns the cache for now's business day, replacing an
// older one. Replacing also prunes hourly rows past the retention window.
func usageDiscountDayFor(location *time.Location, now time.Time) (*usageDiscountDay, error) {
	next, err := newUsageDiscountDay(location, now)
	if err != nil {
		return nil, err
	}
	for {
		current := currentUsageDiscountDay.Load()
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
		if currentUsageDiscountDay.CompareAndSwap(current, next) {
			pruneUsageDiscountHourly(next)
			return next, nil
		}
	}
}

func pruneUsageDiscountHourly(day *usageDiscountDay) {
	before := day.cutoff - int64(usageDiscountRetentionDays)*24*3600
	if err := model.DeleteUsageDiscountHourlyBefore(before); err != nil {
		common.SysError(common.LogText("failed to prune usage discount hourly rows: %v", err))
	}
}

func (d *usageDiscountDay) windowSums(userID int) (model.UsageDiscountWindowSums, error) {
	if sums, ok := d.cachedSums(userID); ok {
		return sums, nil
	}
	loaded, err, _ := d.loads.Do(strconv.Itoa(userID), func() (any, error) {
		// A load that finished just before this one joined already cached it.
		if sums, ok := d.cachedSums(userID); ok {
			return sums, nil
		}
		sums, err := model.SumUsageDiscountWindows(userID, d.dayStart, d.weekStart, d.monthStart, d.cutoff)
		if err != nil {
			return sums, err
		}
		d.mu.Lock()
		d.users[userID] = sums
		d.mu.Unlock()
		return sums, nil
	})
	return loaded.(model.UsageDiscountWindowSums), err
}

func (d *usageDiscountDay) cachedSums(userID int) (model.UsageDiscountWindowSums, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sums, ok := d.users[userID]
	return sums, ok
}

func (d *usageDiscountDay) cachedUsers() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.users)
}

// siteMonthQuota is the 30-day total of users outside the exclusion list, each
// floored at 0. It is cached per config version because exclusions may change.
func (d *usageDiscountDay) siteMonthQuota(settings *model.UsageDiscountSettings, now time.Time) (int64, error) {
	d.siteMu.Lock()
	defer d.siteMu.Unlock()
	if d.siteReady && d.siteVersion == settings.Version {
		return d.siteQuota, nil
	}
	if d.siteErr != nil && now.Sub(d.siteFailedAt) < usageDiscountSiteRetryAfter {
		return 0, d.siteErr
	}
	rows, err := model.SumUsageDiscountByUser(d.monthStart, d.cutoff)
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

// usageDiscountAmounts are eligible window amounts, already floored at 0.
type usageDiscountAmounts struct {
	Site  int64
	Day   int64
	Week  int64
	Month int64
}

// evaluateUsageDiscount applies the formula. A nil amounts means no usage
// component applies (excluded user, components off, or data unavailable).
func evaluateUsageDiscount(settings *model.UsageDiscountSettings, amounts *usageDiscountAmounts) *hosttypes.UsageDiscountDecision {
	decision := &hosttypes.UsageDiscountDecision{
		Status:        hosttypes.UsageDiscountOK,
		Active:        settings.Active(),
		FixedRate:     settings.FixedRate,
		CapRate:       settings.CapRate,
		ConfigVersion: settings.Version,
	}
	if amounts != nil {
		decision.SiteRate = usageDiscountComponentRate(decision, "site", settings.Site, amounts.Site)
		decision.DayRate = usageDiscountComponentRate(decision, usageDiscountWindowDay, settings.Day, amounts.Day)
		decision.WeekRate = usageDiscountComponentRate(decision, usageDiscountWindowWeek, settings.Week, amounts.Week)
		decision.MonthRate = usageDiscountComponentRate(decision, usageDiscountWindowMonth, settings.Month, amounts.Month)
	}
	userRate := decimal.Zero
	// Ties go to the longer window, which is the more stable explanation.
	for _, window := range []struct {
		name string
		rate decimal.Decimal
	}{{usageDiscountWindowMonth, decision.MonthRate}, {usageDiscountWindowWeek, decision.WeekRate}, {usageDiscountWindowDay, decision.DayRate}} {
		if window.rate.GreaterThan(userRate) {
			userRate, decision.WinnerWindow = window.rate, window.name
		}
	}
	decision.RawRate = decision.FixedRate.Add(decision.SiteRate).Add(userRate)
	decision.AppliedRate = decimal.Min(decision.RawRate, settings.CapRate)
	decision.CapApplied = decision.RawRate.GreaterThan(settings.CapRate)
	decision.Factor = decimal.NewFromInt(1).Sub(decision.AppliedRate)
	return decision
}

// usageDiscountComponentRate returns the rate that takes effect; a shadow
// component is recorded for administrators and contributes 0.
func usageDiscountComponentRate(decision *hosttypes.UsageDiscountDecision, name string, table model.UsageDiscountTable, amount int64) decimal.Decimal {
	if table.Mode == model.UsageDiscountModeOff {
		return decimal.Zero
	}
	rate := usageDiscountTierRate(table, amount)
	if table.Mode == model.UsageDiscountModeShadow {
		if decision.ShadowRates == nil {
			decision.ShadowRates = make(map[string]string)
		}
		decision.ShadowRates[name] = rate.String()
		return decimal.Zero
	}
	return rate
}

// usageDiscountTierRate is the rate of the highest tier whose threshold the
// amount reaches; below the first tier it is 0.
func usageDiscountTierRate(table model.UsageDiscountTable, amount int64) decimal.Decimal {
	rate := decimal.Zero
	for _, tier := range table.Tiers {
		if amount < tier.Threshold {
			break
		}
		rate = tier.Rate
	}
	return rate
}

type usageDiscountResolution struct {
	Settings    *model.UsageDiscountSettings
	Decision    *hosttypes.UsageDiscountDecision
	Amounts     usageDiscountAmounts
	WindowsRead bool
}

// ResolveUsageDiscount returns the decision for one logical request. It never
// fails the request: problems are recorded in the decision as unavailable.
func ResolveUsageDiscount(userID int, isChannelTest bool) *hosttypes.UsageDiscountDecision {
	settings, err := model.CurrentUsageDiscountSettings()
	if err != nil {
		common.SysError(common.LogText("usage discount config unavailable: %v", err))
		return hosttypes.NeutralUsageDiscount(hosttypes.UsageDiscountUnavailable, "config_unavailable")
	}
	return resolveUsageDiscount(settings, userID, isChannelTest, time.Now()).Decision
}

func resolveUsageDiscount(settings *model.UsageDiscountSettings, userID int, isChannelTest bool, now time.Time) usageDiscountResolution {
	result := usageDiscountResolution{Settings: settings}
	switch {
	case !settings.Enabled():
		result.Decision = hosttypes.NeutralUsageDiscount(hosttypes.UsageDiscountInactive, "")
		result.Decision.ConfigVersion = settings.Version
		return result
	case isChannelTest:
		result.Decision = hosttypes.NeutralUsageDiscount(hosttypes.UsageDiscountNotApplicable, "channel_test")
		result.Decision.Active, result.Decision.ConfigVersion = settings.Active(), settings.Version
		return result
	case settings.IsExcluded(userID):
		result.Decision = evaluateUsageDiscount(settings, nil)
		result.Decision.Reason = usageDiscountReasonExcluded
		return result
	case !settings.UsesSite() && !settings.UsesWindows():
		result.Decision = evaluateUsageDiscount(settings, nil)
		return result
	}
	amounts, businessDate, err := readUsageDiscountAmounts(settings, userID, now)
	if err != nil {
		common.SysError(common.LogText("usage discount windows unavailable: user_id=%d, error=%v", userID, err))
		result.Decision = evaluateUsageDiscount(settings, nil)
		result.Decision.Status, result.Decision.Reason = hosttypes.UsageDiscountUnavailable, "usage_data_unavailable"
		return result
	}
	result.Amounts, result.WindowsRead = amounts, settings.UsesWindows()
	result.Decision = evaluateUsageDiscount(settings, &amounts)
	result.Decision.BusinessDate = businessDate
	return result
}

func readUsageDiscountAmounts(settings *model.UsageDiscountSettings, userID int, now time.Time) (usageDiscountAmounts, string, error) {
	var amounts usageDiscountAmounts
	day, err := usageDiscountDayFor(settings.Location, now)
	if err != nil {
		return amounts, "", err
	}
	if settings.UsesSite() {
		if amounts.Site, err = day.siteMonthQuota(settings, now); err != nil {
			return amounts, "", err
		}
	}
	if settings.UsesWindows() {
		sums, err := day.windowSums(userID)
		if err != nil {
			return amounts, "", err
		}
		amounts.Day, amounts.Week, amounts.Month = max(sums.DayQuota, 0), max(sums.WeekQuota, 0), max(sums.MonthQuota, 0)
	}
	return amounts, day.date, nil
}

// UsageDiscountSelfView is what a user may see about their own discount: no
// site total, no exclusion list and no shadow components.
type UsageDiscountSelfView struct {
	Status       string                    `json:"status"`
	Factor       string                    `json:"factor"`
	GlobalFactor string                    `json:"global_factor"`
	FixedRate    string                    `json:"fixed_rate"`
	SiteRate     string                    `json:"site_rate"`
	AppliedRate  string                    `json:"applied_rate"`
	CapRate      string                    `json:"cap_rate"`
	CapApplied   bool                      `json:"cap_applied"`
	WinnerWindow string                    `json:"winner_window,omitempty"`
	BusinessDate string                    `json:"business_date,omitempty"`
	Windows      []UsageDiscountWindowView `json:"windows"`
}

type UsageDiscountWindowView struct {
	Window string                          `json:"window"`
	Quota  int64                           `json:"quota"`
	Rate   string                          `json:"rate"`
	Tiers  []model.UsageDiscountTierConfig `json:"tiers"`
}

func GetUsageDiscountSelfView(userID int, now time.Time) (*UsageDiscountSelfView, error) {
	settings, err := model.CurrentUsageDiscountSettings()
	if err != nil {
		return nil, err
	}
	return usageDiscountSelfView(settings, userID, now), nil
}

func usageDiscountSelfView(settings *model.UsageDiscountSettings, userID int, now time.Time) *UsageDiscountSelfView {
	result := resolveUsageDiscount(settings, userID, false, now)
	decision := result.Decision
	view := &UsageDiscountSelfView{
		Status:       decision.Status,
		Factor:       decision.Factor.String(),
		GlobalFactor: decimal.NewFromInt(1).Sub(decimal.Min(decision.FixedRate.Add(decision.SiteRate), decision.CapRate)).String(),
		FixedRate:    decision.FixedRate.String(),
		SiteRate:     decision.SiteRate.String(),
		AppliedRate:  decision.AppliedRate.String(),
		CapRate:      decision.CapRate.String(),
		CapApplied:   decision.CapApplied,
		WinnerWindow: decision.WinnerWindow,
		BusinessDate: decision.BusinessDate,
		Windows:      []UsageDiscountWindowView{},
	}
	if !settings.Active() {
		// Shadow-only configurations change nothing for the user.
		view.Status = hosttypes.UsageDiscountInactive
		return view
	}
	if !result.WindowsRead {
		return view
	}
	windows := []struct {
		name   string
		config model.UsageDiscountTableConfig
		quota  int64
		rate   decimal.Decimal
	}{
		{usageDiscountWindowDay, settings.Config.Personal.Day, result.Amounts.Day, decision.DayRate},
		{usageDiscountWindowWeek, settings.Config.Personal.Week, result.Amounts.Week, decision.WeekRate},
		{usageDiscountWindowMonth, settings.Config.Personal.Month, result.Amounts.Month, decision.MonthRate},
	}
	for _, window := range windows {
		if window.config.Mode == model.UsageDiscountModeOn {
			view.Windows = append(view.Windows, UsageDiscountWindowView{
				Window: window.name, Quota: window.quota, Rate: window.rate.String(), Tiers: window.config.Tiers,
			})
		}
	}
	return view
}

// UsageDiscountStatusView is the Root health view.
type UsageDiscountStatusView struct {
	ConfigVersion  int64                     `json:"config_version"`
	Timezone       string                    `json:"timezone"`
	BusinessDate   string                    `json:"business_date"`
	CutoffUTC      int64                     `json:"cutoff_utc"`
	CachedUsers    int                       `json:"cached_users"`
	SiteMonthQuota int64                     `json:"site_month_quota"`
	SiteTierRate   string                    `json:"site_tier_rate"`
	EarliestHour   *int64                    `json:"earliest_hour_utc"`
	Health         model.UsageDiscountHealth `json:"health"`
}

func GetUsageDiscountStatus(now time.Time) (*UsageDiscountStatusView, error) {
	settings, err := model.CurrentUsageDiscountSettings()
	if err != nil {
		return nil, err
	}
	day, err := usageDiscountDayFor(settings.Location, now)
	if err != nil {
		return nil, err
	}
	siteQuota, err := day.siteMonthQuota(settings, now)
	if err != nil {
		return nil, err
	}
	view := &UsageDiscountStatusView{
		ConfigVersion:  settings.Version,
		Timezone:       day.timezone,
		BusinessDate:   day.date,
		CutoffUTC:      day.cutoff,
		CachedUsers:    day.cachedUsers(),
		SiteMonthQuota: siteQuota,
		SiteTierRate:   usageDiscountTierRate(settings.Site, siteQuota).String(),
		Health:         model.GetUsageDiscountHealth(),
	}
	earliest, ok, err := model.EarliestUsageDiscountHour()
	if err != nil {
		return nil, err
	}
	if ok {
		view.EarliestHour = &earliest
	}
	return view, nil
}
