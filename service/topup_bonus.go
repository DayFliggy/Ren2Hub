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
// exceptions are listed in docs/billing/TOPUP_BONUS.md.

const (
	topUpBonusMonthDays      = 30
	topUpBonusRetentionDays  = 90
	topUpBonusEstimateDays   = 30
	topUpBonusDateLayout     = time.DateOnly
	topUpBonusWindowDay      = "day"
	topUpBonusWindowWeek     = "week"
	topUpBonusWindowMonth    = "month"
	topUpBonusReasonExcluded = "excluded_user"
	// A failing site query is retried at most this often: requests waiting on
	// the site mutex would otherwise each pay the full statement timeout.
	topUpBonusSiteRetryAfter = 10 * time.Second
)

var errTopUpBonusUnalignedDay = errors.New("business day boundary is not a whole UTC hour")

// topUpBonusDay holds one business day's boundaries and cached sums.
type topUpBonusDay struct {
	date       string
	timezone   string
	dayStart   int64
	weekStart  int64
	monthStart int64
	cutoff     int64

	mu    sync.Mutex
	users map[int]model.TopUpBonusWindowSums
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

var currentTopUpBonusDay atomic.Pointer[topUpBonusDay]

// ResetTopUpBonusCaches drops cached settings and window sums, for callers
// that replace the database.
func ResetTopUpBonusCaches() {
	model.ResetTopUpBonusSettingsCache()
	currentTopUpBonusDay.Store(nil)
}

func newTopUpBonusDay(location *time.Location, now time.Time) (*topUpBonusDay, error) {
	local := now.In(location)
	midnight := func(daysBack int) int64 {
		return time.Date(local.Year(), local.Month(), local.Day()-daysBack, 0, 0, 0, 0, location).Unix()
	}
	day := &topUpBonusDay{
		date:       local.Format(topUpBonusDateLayout),
		timezone:   location.String(),
		cutoff:     midnight(0),
		dayStart:   midnight(1),
		weekStart:  midnight(7),
		monthStart: midnight(topUpBonusMonthDays),
		users:      make(map[int]model.TopUpBonusWindowSums),
	}
	for _, boundary := range []int64{day.cutoff, day.dayStart, day.weekStart, day.monthStart} {
		if boundary%3600 != 0 {
			return nil, fmt.Errorf("%w: %s on %s", errTopUpBonusUnalignedDay, day.timezone, day.date)
		}
	}
	return day, nil
}

// topUpBonusDayFor returns the cache for now's business day, replacing an
// older one. Replacing also prunes hourly rows past the retention window.
func topUpBonusDayFor(location *time.Location, now time.Time) (*topUpBonusDay, error) {
	next, err := newTopUpBonusDay(location, now)
	if err != nil {
		return nil, err
	}
	for {
		current := currentTopUpBonusDay.Load()
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
		if currentTopUpBonusDay.CompareAndSwap(current, next) {
			pruneTopUpBonusUsageHourly(next)
			return next, nil
		}
	}
}

func pruneTopUpBonusUsageHourly(day *topUpBonusDay) {
	before := day.cutoff - int64(topUpBonusRetentionDays)*24*3600
	if err := model.DeleteTopUpBonusUsageHourlyBefore(before); err != nil {
		common.SysError(common.LogText("failed to prune top-up bonus usage rows: %v", err))
	}
}

func (d *topUpBonusDay) windowSums(userID int) (model.TopUpBonusWindowSums, error) {
	if sums, ok := d.cachedSums(userID); ok {
		return sums, nil
	}
	loaded, err, _ := d.loads.Do(strconv.Itoa(userID), func() (any, error) {
		// A load that finished just before this one joined already cached it.
		if sums, ok := d.cachedSums(userID); ok {
			return sums, nil
		}
		sums, err := model.SumTopUpBonusWindows(userID, d.dayStart, d.weekStart, d.monthStart, d.cutoff)
		if err != nil {
			return sums, err
		}
		d.mu.Lock()
		d.users[userID] = sums
		d.mu.Unlock()
		return sums, nil
	})
	return loaded.(model.TopUpBonusWindowSums), err
}

func (d *topUpBonusDay) cachedSums(userID int) (model.TopUpBonusWindowSums, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sums, ok := d.users[userID]
	return sums, ok
}

func (d *topUpBonusDay) cachedUsers() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.users)
}

// siteMonthQuota is the 30-day total of users outside the exclusion list, each
// floored at 0. It is cached per config version because exclusions may change.
func (d *topUpBonusDay) siteMonthQuota(settings *model.TopUpBonusSettings, now time.Time) (int64, error) {
	d.siteMu.Lock()
	defer d.siteMu.Unlock()
	if d.siteReady && d.siteVersion == settings.Version {
		return d.siteQuota, nil
	}
	if d.siteErr != nil && now.Sub(d.siteFailedAt) < topUpBonusSiteRetryAfter {
		return 0, d.siteErr
	}
	rows, err := model.SumTopUpBonusByUser(d.monthStart, d.cutoff)
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

// topUpBonusAmounts are the inputs of one evaluation. A nil pointer means
// the value was not read (component off, user excluded or the query failed);
// it is never treated as zero usage. A nil Principal is a view without a
// credit: the amount tier is skipped, even a zero-threshold tier.
type topUpBonusAmounts struct {
	Principal *int64
	Site      *int64
	Personal  *model.TopUpBonusWindowSums
}

// evaluateTopUpBonus applies the formula once:
// rate = min(amount + fixed + site + max(day, week, month), cap) and
// bonus = floor(principal × rate).
func evaluateTopUpBonus(settings *model.TopUpBonusSettings, amounts topUpBonusAmounts) *TopUpBonusDecision {
	decision := &TopUpBonusDecision{
		Status:        TopUpBonusOK,
		FixedRate:     settings.FixedRate,
		CapRate:       settings.CapRate,
		ConfigVersion: settings.Version,
	}
	decision.AmountRate = topUpBonusComponentRate(decision, "amount", settings.Amount, amounts.Principal)
	decision.SiteRate = topUpBonusComponentRate(decision, "site", settings.Site, amounts.Site)
	var day, week, month *int64
	if amounts.Personal != nil {
		day, week, month = &amounts.Personal.DayQuota, &amounts.Personal.WeekQuota, &amounts.Personal.MonthQuota
	}
	decision.DayRate = topUpBonusComponentRate(decision, topUpBonusWindowDay, settings.Day, day)
	decision.WeekRate = topUpBonusComponentRate(decision, topUpBonusWindowWeek, settings.Week, week)
	decision.MonthRate = topUpBonusComponentRate(decision, topUpBonusWindowMonth, settings.Month, month)
	userRate := decimal.Zero
	// Ties go to the longer window, which is the more stable explanation.
	for _, window := range []struct {
		name string
		rate decimal.Decimal
	}{{topUpBonusWindowMonth, decision.MonthRate}, {topUpBonusWindowWeek, decision.WeekRate}, {topUpBonusWindowDay, decision.DayRate}} {
		if window.rate.GreaterThan(userRate) {
			userRate, decision.WinnerWindow = window.rate, window.name
		}
	}
	decision.RawRate = decision.AmountRate.Add(decision.FixedRate).Add(decision.SiteRate).Add(userRate)
	decision.AppliedRate = decimal.Min(decision.RawRate, settings.CapRate)
	decision.CapApplied = decision.RawRate.GreaterThan(settings.CapRate)
	if amounts.Principal != nil {
		decision.PrincipalQuota = *amounts.Principal
		decision.BonusQuota = decimal.NewFromInt(*amounts.Principal).Mul(decision.AppliedRate).Floor().IntPart()
	}
	return decision
}

// topUpBonusComponentRate returns the rate that takes effect. A shadow
// component is recorded for administrators and contributes 0; an unread
// amount contributes 0 (the caller has already rejected unread "on" tables).
func topUpBonusComponentRate(decision *TopUpBonusDecision, name string, table model.TopUpBonusTable, amount *int64) decimal.Decimal {
	if table.Mode == model.TopUpBonusModeOff || amount == nil {
		return decimal.Zero
	}
	rate := topUpBonusTierRate(table, max(*amount, 0))
	if table.Mode == model.TopUpBonusModeShadow {
		if decision.ShadowRates == nil {
			decision.ShadowRates = make(map[string]string)
		}
		decision.ShadowRates[name] = rate.String()
		return decimal.Zero
	}
	return rate
}

// topUpBonusTierRate is the rate of the highest tier whose threshold the
// amount reaches; below the first tier it is 0.
func topUpBonusTierRate(table model.TopUpBonusTable, amount int64) decimal.Decimal {
	rate := decimal.Zero
	for _, tier := range table.Tiers {
		if amount < tier.Threshold {
			break
		}
		rate = tier.Rate
	}
	return rate
}

// TopUpBonusContext holds the inputs of one user's bonus read at one moment.
// Evaluating it is pure, so a credit may evaluate it inside its transaction.
type TopUpBonusContext struct {
	settings       *model.TopUpBonusSettings
	site           *int64
	personal       *model.TopUpBonusWindowSums
	businessDate   string
	excluded       bool
	shadowFailures []string
}

// LoadTopUpBonusContext reads the current configuration and the user's
// windows. It fails only when a table that grants a bonus cannot be read; a
// failed shadow-only source is recorded and the other components still apply.
func LoadTopUpBonusContext(userID int, now time.Time) (*TopUpBonusContext, error) {
	settings, err := model.CurrentTopUpBonusSettings()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTopUpBonusUnavailable, err)
	}
	return loadTopUpBonusContext(settings, userID, now)
}

func loadTopUpBonusContext(settings *model.TopUpBonusSettings, userID int, now time.Time) (*TopUpBonusContext, error) {
	ctx := &TopUpBonusContext{settings: settings, excluded: settings.IsExcluded(userID)}
	if !settings.Enabled() || ctx.excluded || (!settings.UsesSite() && !settings.UsesWindows()) {
		return ctx, nil
	}
	day, err := topUpBonusDayFor(settings.Location, now)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTopUpBonusUnavailable, err)
	}
	ctx.businessDate = day.date
	if settings.UsesSite() {
		site, err := day.siteMonthQuota(settings, now)
		switch {
		case err == nil:
			ctx.site = &site
		case settings.Site.Mode == model.TopUpBonusModeOn:
			common.SysError(common.LogText("top-up bonus site total unavailable: %v", err))
			return nil, fmt.Errorf("%w: %v", ErrTopUpBonusUnavailable, err)
		default:
			ctx.shadowFailures = append(ctx.shadowFailures, "site")
		}
	}
	if settings.UsesWindows() {
		sums, err := day.windowSums(userID)
		switch {
		case err == nil:
			ctx.personal = &sums
		case settings.PersonalOn():
			common.SysError(common.LogText("top-up bonus windows unavailable: user_id=%d, error=%v", userID, err))
			return nil, fmt.Errorf("%w: %v", ErrTopUpBonusUnavailable, err)
		default:
			ctx.shadowFailures = append(ctx.shadowFailures, "personal")
		}
	}
	return ctx, nil
}

// Evaluate decides the bonus of one credit of principal quota.
func (c *TopUpBonusContext) Evaluate(principal int) *TopUpBonusDecision {
	value := int64(principal)
	return c.evaluate(&value)
}

// evaluate decides the bonus; a nil principal is a view without a credit.
func (c *TopUpBonusContext) evaluate(principal *int64) *TopUpBonusDecision {
	if !c.settings.Enabled() {
		decision := neutralTopUpBonus(TopUpBonusInactive)
		decision.ConfigVersion = c.settings.Version
		if principal != nil {
			decision.PrincipalQuota = *principal
		}
		return decision
	}
	decision := evaluateTopUpBonus(c.settings, topUpBonusAmounts{Principal: principal, Site: c.site, Personal: c.personal})
	decision.BusinessDate = c.businessDate
	switch {
	case c.excluded:
		decision.Reason = topUpBonusReasonExcluded
	case len(c.shadowFailures) > 0:
		decision.Status, decision.Reason = TopUpBonusShadowUnavailable, strings.Join(c.shadowFailures, ",")
	}
	return decision
}

// Grant decides the bonus of one credit and freezes its explanation. Nothing
// is recorded when no component is configured.
func (c *TopUpBonusContext) Grant(principal int) (model.TopUpBonusGrant, error) {
	return topUpBonusGrant(c.Evaluate(principal))
}

func topUpBonusGrant(decision *TopUpBonusDecision) (model.TopUpBonusGrant, error) {
	grant := model.TopUpBonusGrant{BonusQuota: int(decision.BonusQuota), ConfigVersion: decision.ConfigVersion}
	if decision.Status == TopUpBonusInactive {
		return grant, nil
	}
	encoded, err := common.Marshal(decision)
	if err != nil {
		return model.TopUpBonusGrant{}, err
	}
	grant.Decision = string(encoded)
	return grant, nil
}

// TopUpBonusEvaluator prepares the bonus of one credit before its
// transaction. A failed load is returned only if the credit is evaluated, so
// a replayed operation never depends on the bonus inputs.
func TopUpBonusEvaluator(userID int, now time.Time) model.TopUpBonusEvaluator {
	ctx, err := LoadTopUpBonusContext(userID, now)
	return func(principal int) (model.TopUpBonusGrant, error) {
		if err != nil {
			return model.TopUpBonusGrant{}, err
		}
		return ctx.Grant(principal)
	}
}

// TopUpBonusSelfView is what a user may see about the bonus of their next
// credit: no site total, no exclusion list and no shadow components. The
// amount tier depends on the credit, so it is listed rather than applied.
type TopUpBonusSelfView struct {
	Status       string                       `json:"status"`
	FixedRate    string                       `json:"fixed_rate"`
	SiteRate     string                       `json:"site_rate"`
	PersonalRate string                       `json:"personal_rate"`
	RawRate      string                       `json:"raw_rate"`
	AppliedRate  string                       `json:"applied_rate"`
	CapRate      string                       `json:"cap_rate"`
	CapApplied   bool                         `json:"cap_applied"`
	WinnerWindow string                       `json:"winner_window,omitempty"`
	BusinessDate string                       `json:"business_date,omitempty"`
	AmountTiers  []model.TopUpBonusTierConfig `json:"amount_tiers"`
	Windows      []TopUpBonusWindowView       `json:"windows"`
}

type TopUpBonusWindowView struct {
	Window string                       `json:"window"`
	Quota  int64                        `json:"quota"`
	Rate   string                       `json:"rate"`
	Tiers  []model.TopUpBonusTierConfig `json:"tiers"`
}

func GetTopUpBonusSelfView(userID int, now time.Time) (*TopUpBonusSelfView, error) {
	settings, err := model.CurrentTopUpBonusSettings()
	if err != nil {
		return nil, err
	}
	return topUpBonusSelfView(settings, userID, now)
}

// topUpBonusSelfView evaluates every component except the credit-dependent
// amount tier, which is listed in AmountTiers instead, even a 0 threshold.
func topUpBonusSelfView(settings *model.TopUpBonusSettings, userID int, now time.Time) (*TopUpBonusSelfView, error) {
	view := &TopUpBonusSelfView{
		Status: TopUpBonusInactive, FixedRate: "0", SiteRate: "0", PersonalRate: "0", RawRate: "0", AppliedRate: "0",
		CapRate: settings.CapRate.String(), AmountTiers: []model.TopUpBonusTierConfig{}, Windows: []TopUpBonusWindowView{},
	}
	if !settings.Active() {
		// Shadow-only configurations change nothing for the user.
		return view, nil
	}
	ctx, err := loadTopUpBonusContext(settings, userID, now)
	if err != nil {
		return nil, err
	}
	decision := ctx.evaluate(nil)
	view.Status = decision.Status
	if view.Status == TopUpBonusShadowUnavailable {
		view.Status = TopUpBonusOK
	}
	view.FixedRate, view.SiteRate = decision.FixedRate.String(), decision.SiteRate.String()
	view.PersonalRate = decimal.Max(decision.DayRate, decision.WeekRate, decision.MonthRate).String()
	view.RawRate, view.AppliedRate, view.CapApplied = decision.RawRate.String(), decision.AppliedRate.String(), decision.CapApplied
	view.WinnerWindow, view.BusinessDate = decision.WinnerWindow, decision.BusinessDate
	if settings.Amount.Mode == model.TopUpBonusModeOn {
		view.AmountTiers = settings.Config.Amount.Tiers
	}
	if ctx.personal == nil {
		return view, nil
	}
	for _, window := range []struct {
		name   string
		config model.TopUpBonusTableConfig
		quota  int64
		rate   decimal.Decimal
	}{
		{topUpBonusWindowDay, settings.Config.Personal.Day, ctx.personal.DayQuota, decision.DayRate},
		{topUpBonusWindowWeek, settings.Config.Personal.Week, ctx.personal.WeekQuota, decision.WeekRate},
		{topUpBonusWindowMonth, settings.Config.Personal.Month, ctx.personal.MonthQuota, decision.MonthRate},
	} {
		if window.config.Mode == model.TopUpBonusModeOn {
			view.Windows = append(view.Windows, TopUpBonusWindowView{
				Window: window.name, Quota: max(window.quota, 0), Rate: window.rate.String(), Tiers: window.config.Tiers,
			})
		}
	}
	return view, nil
}

// TopUpBonusStatusView is the Root health view.
type TopUpBonusStatusView struct {
	ConfigVersion  int64                  `json:"config_version"`
	Timezone       string                 `json:"timezone"`
	BusinessDate   string                 `json:"business_date"`
	CutoffUTC      int64                  `json:"cutoff_utc"`
	CachedUsers    int                    `json:"cached_users"`
	SiteMonthQuota int64                  `json:"site_month_quota"`
	SiteTierRate   string                 `json:"site_tier_rate"`
	EarliestHour   *int64                 `json:"earliest_hour_utc"`
	Health         model.TopUpBonusHealth `json:"health"`
}

func GetTopUpBonusStatus(now time.Time) (*TopUpBonusStatusView, error) {
	settings, err := model.CurrentTopUpBonusSettings()
	if err != nil {
		return nil, err
	}
	day, err := topUpBonusDayFor(settings.Location, now)
	if err != nil {
		return nil, err
	}
	siteQuota, err := day.siteMonthQuota(settings, now)
	if err != nil {
		return nil, err
	}
	view := &TopUpBonusStatusView{
		ConfigVersion:  settings.Version,
		Timezone:       day.timezone,
		BusinessDate:   day.date,
		CutoffUTC:      day.cutoff,
		CachedUsers:    day.cachedUsers(),
		SiteMonthQuota: siteQuota,
		SiteTierRate:   topUpBonusTierRate(settings.Site, siteQuota).String(),
		Health:         model.GetTopUpBonusHealth(),
	}
	earliest, ok, err := model.EarliestTopUpBonusHour()
	if err != nil {
		return nil, err
	}
	if ok {
		view.EarliestHour = &earliest
	}
	return view, nil
}
