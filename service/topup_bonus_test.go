package service

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Thursday 2026-10-30 18:00 in Asia/Shanghai; the cutoff is 2026-10-29T16:00Z.
var topUpBonusTestNow = time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

const topUpBonusTestCutoff = int64(1793289600)

func topUpBonusTiers(pairs ...string) []model.TopUpBonusTierConfig {
	tiers := []model.TopUpBonusTierConfig{}
	for _, pair := range pairs {
		threshold, rate, _ := strings.Cut(pair, ":")
		tiers = append(tiers, model.TopUpBonusTierConfig{ThresholdQuota: threshold, Rate: rate})
	}
	return tiers
}

// topUpBonusTestSettings: fixed 1%, cap 5%, site 1000→1% / 5000→2%,
// day 100→3%, week 500→4%, month 1000→5%, all on, user 1 excluded.
func topUpBonusTestSettings(t *testing.T, version int64, mutate func(*model.TopUpBonusConfig)) *model.TopUpBonusSettings {
	t.Helper()
	config := model.DefaultTopUpBonusConfig()
	config.FixedRate = "0.01"
	config.Site = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("1000:0.01", "5000:0.02")}
	config.Personal.Day = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("100:0.03")}
	config.Personal.Week = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("500:0.04")}
	config.Personal.Month = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("1000:0.05")}
	if mutate != nil {
		mutate(&config)
	}
	settings, err := model.BuildTopUpBonusSettings(config)
	require.NoError(t, err)
	settings.Version = version
	return settings
}

func setupTopUpBonusTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.TopUpBonusUsageHourly{}, &model.TopUpBonusConfigVersion{}))
	previousDB, previousDay := model.DB, currentTopUpBonusDay.Load()
	model.DB = db
	ResetTopUpBonusCaches()
	t.Cleanup(func() {
		model.DB = previousDB
		ResetTopUpBonusCaches()
		currentTopUpBonusDay.Store(previousDay)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func seedTopUpBonusRows(t *testing.T, db *gorm.DB, rows ...model.TopUpBonusUsageHourly) {
	t.Helper()
	for _, row := range rows {
		require.NoError(t, db.Create(&row).Error)
	}
}

func i64(v int64) *int64 { return &v }

func windows(day, week, month int64) *model.TopUpBonusWindowSums {
	return &model.TopUpBonusWindowSums{DayQuota: day, WeekQuota: week, MonthQuota: month}
}

func TestEvaluateTopUpBonusFormula(t *testing.T) {
	settings := topUpBonusTestSettings(t, 1, nil)
	decision := evaluateTopUpBonus(settings, topUpBonusAmounts{Principal: i64(1999), Site: i64(5000), Personal: windows(100, 500, 1000)})
	assert.Equal(t, "0.08", decision.RawRate.String(), "0.01 + 0.02 + max(0.03, 0.04, 0.05)")
	assert.Equal(t, "0.05", decision.AppliedRate.String())
	assert.True(t, decision.CapApplied)
	assert.Equal(t, int64(1999), decision.PrincipalQuota)
	assert.Equal(t, int64(99), decision.BonusQuota, "floor(1999 × 0.05)")
	assert.Equal(t, topUpBonusWindowMonth, decision.WinnerWindow)
	assert.Zero(t, evaluateTopUpBonus(settings, topUpBonusAmounts{Principal: i64(19), Site: i64(5000)}).BonusQuota, "a bonus below one quota is 0")
	assert.Zero(t, evaluateTopUpBonus(settings, topUpBonusAmounts{Site: i64(5000)}).BonusQuota, "a view without a credit has no bonus")

	shadow := topUpBonusTestSettings(t, 1, func(c *model.TopUpBonusConfig) {
		c.Site.Mode, c.Personal.Month.Mode = model.TopUpBonusModeOff, model.TopUpBonusModeShadow
	})
	decision = evaluateTopUpBonus(shadow, topUpBonusAmounts{Site: i64(5000), Personal: windows(100, 500, 1000)})
	assert.Equal(t, "0.05", decision.RawRate.String(), "shadow month and off site contribute 0")
	assert.False(t, decision.CapApplied)
	assert.Equal(t, topUpBonusWindowWeek, decision.WinnerWindow)
	assert.Equal(t, map[string]string{topUpBonusWindowMonth: "0.05"}, decision.ShadowRates)

	below := evaluateTopUpBonus(settings, topUpBonusAmounts{Site: i64(999), Personal: windows(99, 499, 999)})
	assert.Equal(t, "0.01", below.AppliedRate.String(), "below every first tier only fixed applies")
	assert.Empty(t, below.WinnerWindow)
	assert.Equal(t, below.AppliedRate, evaluateTopUpBonus(settings, topUpBonusAmounts{}).AppliedRate, "unread sources contribute 0")

	tie := topUpBonusTestSettings(t, 1, func(c *model.TopUpBonusConfig) {
		c.Personal.Day.Tiers = topUpBonusTiers("100:0.05")
	})
	assert.Equal(t, topUpBonusWindowMonth, evaluateTopUpBonus(tie, topUpBonusAmounts{Personal: windows(100, 0, 1000)}).WinnerWindow,
		"ties go to the longer window")

	// The amount tier is keyed on the order's credited quota and joins the sum.
	amount := topUpBonusTestSettings(t, 1, func(c *model.TopUpBonusConfig) {
		c.CapRate = "0.1"
		c.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("1000:0.02", "5000:0.03")}
	})
	decision = evaluateTopUpBonus(amount, topUpBonusAmounts{Principal: i64(4999), Personal: windows(100, 0, 0)})
	assert.Equal(t, "0.02", decision.AmountRate.String())
	assert.Equal(t, "0.06", decision.AppliedRate.String(), "amount 0.02 + fixed 0.01 + day 0.03")
	assert.Equal(t, "0.03", evaluateTopUpBonus(amount, topUpBonusAmounts{Principal: i64(5000)}).AmountRate.String())
	assert.Equal(t, "0", evaluateTopUpBonus(amount, topUpBonusAmounts{}).AmountRate.String(), "no credit, no amount tier")
}

func seedTopUpBonusResolveRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	cutoff := topUpBonusTestCutoff
	seedTopUpBonusRows(t, db,
		model.TopUpBonusUsageHourly{UserID: 7, HourUTC: cutoff, Quota: 1_000_000}, // today never counts
		model.TopUpBonusUsageHourly{UserID: 7, HourUTC: cutoff - 3600, Quota: 100},
		model.TopUpBonusUsageHourly{UserID: 7, HourUTC: cutoff - 3*86400, Quota: 400},
		model.TopUpBonusUsageHourly{UserID: 7, HourUTC: cutoff - 20*86400, Quota: 500},
		model.TopUpBonusUsageHourly{UserID: 8, HourUTC: cutoff - 3600, Quota: -5000}, // negative rows (task refunds under the old rule) floor at 0
		model.TopUpBonusUsageHourly{UserID: 9, HourUTC: cutoff - 3600, Quota: 3000},
		model.TopUpBonusUsageHourly{UserID: 1, HourUTC: cutoff - 3600, Quota: 1000}, // excluded from the site total
	)
}

type topUpBonusResolution struct {
	Decision *TopUpBonusDecision
	Personal *model.TopUpBonusWindowSums
}

// resolveTopUpBonus loads the user's context and decides a credit of principal.
func resolveTopUpBonus(settings *model.TopUpBonusSettings, userID int, principal int, now time.Time) (topUpBonusResolution, error) {
	ctx, err := loadTopUpBonusContext(settings, userID, now)
	if err != nil {
		return topUpBonusResolution{}, err
	}
	return topUpBonusResolution{Decision: ctx.Evaluate(principal), Personal: ctx.personal}, nil
}

func mustResolve(t *testing.T, settings *model.TopUpBonusSettings, userID int, principal int, now time.Time) topUpBonusResolution {
	t.Helper()
	result, err := resolveTopUpBonus(settings, userID, principal, now)
	require.NoError(t, err)
	return result
}

func TestResolveTopUpBonusFromHourlyWindows(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	seedTopUpBonusResolveRows(t, db)
	settings := topUpBonusTestSettings(t, 1, nil)

	// Site total = 1000 (user 7) + 0 (user 8 floored) + 3000 (user 9) = 4000 → 1%.
	// Counting user 1 would reach 5000 → 2%; counting user 8 raw would drop below 1000.
	user7 := mustResolve(t, settings, 7, 10000, topUpBonusTestNow)
	assert.Equal(t, windows(100, 500, 1000), user7.Personal)
	assert.Equal(t, TopUpBonusOK, user7.Decision.Status)
	assert.Equal(t, "0.01", user7.Decision.SiteRate.String())
	assert.Equal(t, "0.07", user7.Decision.RawRate.String())
	assert.Equal(t, int64(500), user7.Decision.BonusQuota, "capped at 5% of the principal")
	assert.Equal(t, "2026-10-30", user7.Decision.BusinessDate)

	user8 := mustResolve(t, settings, 8, 0, topUpBonusTestNow)
	assert.Equal(t, "0.02", user8.Decision.AppliedRate.String(), "a negative window floors at 0")

	amount := topUpBonusTestSettings(t, 2, func(c *model.TopUpBonusConfig) {
		c.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("1000:0.02")}
	})
	excluded := mustResolve(t, amount, 1, 1000, topUpBonusTestNow)
	assert.Equal(t, "0.03", excluded.Decision.AppliedRate.String(), "excluded users keep the fixed and amount rates")
	assert.Equal(t, topUpBonusReasonExcluded, excluded.Decision.Reason)
	assert.Nil(t, excluded.Personal)

	inactive := topUpBonusTestSettings(t, 3, func(c *model.TopUpBonusConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpBonusModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpBonusModeOff, model.TopUpBonusModeOff, model.TopUpBonusModeOff
	})
	decision := mustResolve(t, inactive, 7, 1000, topUpBonusTestNow).Decision
	assert.Equal(t, TopUpBonusInactive, decision.Status)
	assert.Equal(t, int64(1000), decision.PrincipalQuota)
	assert.Zero(t, decision.BonusQuota)
}

// A table that grants a bonus must be read; a shadow-only source may fail
// without blocking the live components.
func TestResolveTopUpBonusFailureRules(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&model.TopUpBonusUsageHourly{}))
	before := model.GetTopUpBonusHealth().WindowQueryFailures.Count

	_, err := resolveTopUpBonus(topUpBonusTestSettings(t, 1, nil), 7, 0, topUpBonusTestNow)
	assert.ErrorIs(t, err, ErrTopUpBonusUnavailable, "a live site table cannot be skipped")
	assert.Greater(t, model.GetTopUpBonusHealth().WindowQueryFailures.Count, before)

	personalOn := topUpBonusTestSettings(t, 2, func(c *model.TopUpBonusConfig) { c.Site.Mode = model.TopUpBonusModeShadow })
	_, err = resolveTopUpBonus(personalOn, 7, 0, topUpBonusTestNow.Add(topUpBonusSiteRetryAfter))
	assert.ErrorIs(t, err, ErrTopUpBonusUnavailable, "a live personal window cannot be skipped")

	shadowOnly := topUpBonusTestSettings(t, 3, func(c *model.TopUpBonusConfig) {
		c.Site.Mode = model.TopUpBonusModeShadow
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpBonusModeShadow, model.TopUpBonusModeOff, model.TopUpBonusModeOff
		c.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("10:0.02")}
	})
	decision := mustResolve(t, shadowOnly, 7, 1000, topUpBonusTestNow.Add(2*topUpBonusSiteRetryAfter)).Decision
	assert.Equal(t, TopUpBonusShadowUnavailable, decision.Status)
	assert.Equal(t, "site,personal", decision.Reason)
	assert.Equal(t, int64(30), decision.BonusQuota, "fixed and amount still apply")

	excluded := mustResolve(t, topUpBonusTestSettings(t, 4, nil), 1, 0, topUpBonusTestNow)
	assert.Equal(t, TopUpBonusOK, excluded.Decision.Status, "excluded users never read usage")

	require.NoError(t, db.AutoMigrate(&model.TopUpBonusUsageHourly{}))
	failures := model.GetTopUpBonusHealth().WindowQueryFailures.Count
	_, err = resolveTopUpBonus(topUpBonusTestSettings(t, 1, nil), 7, 0, topUpBonusTestNow.Add(2*topUpBonusSiteRetryAfter+time.Second))
	assert.ErrorIs(t, err, ErrTopUpBonusUnavailable, "the site query backs off briefly")
	assert.Equal(t, failures, model.GetTopUpBonusHealth().WindowQueryFailures.Count, "no query runs during the backoff")
	retried := mustResolve(t, topUpBonusTestSettings(t, 1, nil), 7, 0, topUpBonusTestNow.Add(3*topUpBonusSiteRetryAfter+time.Second))
	assert.Equal(t, TopUpBonusOK, retried.Decision.Status, "a failed read is retried, not cached")
}

func TestTopUpBonusDayIsStableAndRollsOver(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	seedTopUpBonusResolveRows(t, db)
	settings := topUpBonusTestSettings(t, 1, nil)
	first := mustResolve(t, settings, 7, 0, topUpBonusTestNow).Personal

	// A write during the business day lands at or after the cutoff.
	seedTopUpBonusRows(t, db, model.TopUpBonusUsageHourly{UserID: 7, HourUTC: topUpBonusTestCutoff + 3600, Quota: 900})
	assert.Equal(t, first, mustResolve(t, settings, 7, 0, topUpBonusTestNow.Add(5*time.Hour)).Personal)
	currentTopUpBonusDay.Store(nil)
	assert.Equal(t, first, mustResolve(t, settings, 7, 0, topUpBonusTestNow).Personal,
		"recomputing after a restart gives the same windows")

	// The next business day counts yesterday's rows, including the cutoff hour.
	next := mustResolve(t, settings, 7, 0, topUpBonusTestNow.Add(24*time.Hour))
	assert.Equal(t, "2026-10-31", next.Decision.BusinessDate)
	assert.Equal(t, int64(1_000_900), next.Personal.DayQuota)
	assert.Equal(t, int64(1_001_900), next.Personal.MonthQuota)

	utc := topUpBonusTestSettings(t, 2, func(c *model.TopUpBonusConfig) { c.Timezone = "UTC" })
	mustResolve(t, utc, 7, 0, topUpBonusTestNow.Add(24*time.Hour))
	assert.Equal(t, "UTC", currentTopUpBonusDay.Load().timezone, "a timezone change replaces the day")
}

func TestTopUpBonusSiteTotalFollowsConfigVersion(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	seedTopUpBonusResolveRows(t, db)
	excluding := topUpBonusTestSettings(t, 1, nil)
	including := topUpBonusTestSettings(t, 2, func(c *model.TopUpBonusConfig) { c.ExcludedUserIDs = []int{} })
	assert.Equal(t, "0.01", mustResolve(t, excluding, 9, 0, topUpBonusTestNow).Decision.SiteRate.String(), "4000 without user 1")
	assert.Equal(t, "0.02", mustResolve(t, including, 9, 0, topUpBonusTestNow).Decision.SiteRate.String(), "5000 with user 1")
}

func TestResolveTopUpBonusConcurrentAcrossMidnight(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	seedTopUpBonusResolveRows(t, db)
	settings := topUpBonusTestSettings(t, 1, nil)
	midnight := time.Unix(topUpBonusTestCutoff+86400, 0)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now, want := midnight.Add(-time.Second), "2026-10-30"
			if i%2 == 1 {
				now, want = midnight, "2026-10-31"
			}
			result, err := resolveTopUpBonus(settings, 7+i%3, 0, now)
			assert.NoError(t, err)
			assert.Equal(t, want, result.Decision.BusinessDate)
		}(i)
	}
	wg.Wait()
	assert.Equal(t, "2026-10-31", currentTopUpBonusDay.Load().date, "the shared day never moves backwards")
}

// Concurrent first reads of one user share a single query, so a write that
// commits between two queries cannot give requests of one day different sums.
func TestTopUpBonusConcurrentFirstReadsShareOneQuery(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	seedTopUpBonusRows(t, db, model.TopUpBonusUsageHourly{UserID: 7, HourUTC: topUpBonusTestCutoff - 3600, Quota: 700})
	var queries atomic.Int32
	release := make(chan struct{})
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:count_window_sums", func(tx *gorm.DB) {
		if tx.Statement.Table == "topup_bonus_usage_hourly" && queries.Add(1) == 1 {
			<-release
		}
	}))
	day, err := newTopUpBonusDay(topUpBonusTestSettings(t, 1, nil).Location, topUpBonusTestNow)
	require.NoError(t, err)

	const readers = 20
	results := make([]model.TopUpBonusWindowSums, readers)
	var started, done sync.WaitGroup
	for i := range readers {
		started.Add(1)
		done.Add(1)
		go func() {
			defer done.Done()
			started.Done()
			sums, err := day.windowSums(7)
			assert.NoError(t, err)
			results[i] = sums
		}()
	}
	started.Wait()
	time.Sleep(50 * time.Millisecond)
	close(release)
	done.Wait()

	assert.Equal(t, int32(1), queries.Load())
	for _, sums := range results {
		assert.Equal(t, model.TopUpBonusWindowSums{DayQuota: 700, WeekQuota: 700, MonthQuota: 700}, sums)
	}
}

func TestTopUpBonusSelfViewHidesSiteTotalAndShadow(t *testing.T) {
	db := setupTopUpBonusTestDB(t)
	seedTopUpBonusResolveRows(t, db)
	settings := topUpBonusTestSettings(t, 1, func(c *model.TopUpBonusConfig) {
		c.Personal.Day.Mode = model.TopUpBonusModeShadow
		c.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("0:0.01", "1000:0.02")}
	})
	view, err := topUpBonusSelfView(settings, 7, topUpBonusTestNow)
	require.NoError(t, err)
	assert.Equal(t, TopUpBonusOK, view.Status)
	assert.Equal(t, "0.01", view.FixedRate)
	assert.Equal(t, "0.01", view.SiteRate)
	assert.Equal(t, "0.05", view.PersonalRate)
	assert.Equal(t, "0.07", view.RawRate, "amount tiers are listed, not applied, even from a 0 threshold")
	assert.Equal(t, "0.05", view.AppliedRate)
	assert.Equal(t, topUpBonusTiers("0:0.01", "1000:0.02"), view.AmountTiers)
	require.Len(t, view.Windows, 2, "the shadow day window is not shown")
	assert.Equal(t, TopUpBonusWindowView{Window: topUpBonusWindowWeek, Quota: 500, Rate: "0.04", Tiers: topUpBonusTiers("500:0.04")}, view.Windows[0])
	assert.Equal(t, int64(1000), view.Windows[1].Quota)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "4000", "the site total stays private")
	assert.NotContains(t, string(encoded), "shadow")

	shadowOnly := topUpBonusTestSettings(t, 2, func(c *model.TopUpBonusConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpBonusModeShadow
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpBonusModeShadow, model.TopUpBonusModeShadow, model.TopUpBonusModeShadow
	})
	view, err = topUpBonusSelfView(shadowOnly, 7, topUpBonusTestNow)
	require.NoError(t, err)
	assert.Equal(t, TopUpBonusInactive, view.Status)
	assert.Equal(t, "0", view.AppliedRate)
	assert.Empty(t, view.Windows)

	amountOnly := topUpBonusTestSettings(t, 3, func(c *model.TopUpBonusConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpBonusModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpBonusModeOff, model.TopUpBonusModeOff, model.TopUpBonusModeOff
		c.Amount = model.TopUpBonusTableConfig{Mode: model.TopUpBonusModeOn, Tiers: topUpBonusTiers("0:0.02")}
	})
	view, err = topUpBonusSelfView(amountOnly, 7, topUpBonusTestNow)
	require.NoError(t, err)
	assert.Equal(t, "0", view.AppliedRate, "the amount tier depends on the credit and is never folded in")
	assert.Equal(t, topUpBonusTiers("0:0.02"), view.AmountTiers)

	excluded, err := topUpBonusSelfView(settings, 1, topUpBonusTestNow)
	require.NoError(t, err)
	assert.Equal(t, "0.01", excluded.AppliedRate, "fixed only; the 0-threshold amount tier is listed")
	assert.Empty(t, excluded.Windows, "excluded users have no personal windows")
}

// saveTopUpBonusConfig stores settings as the next version through the
// normal save path, so QuoteTopUp reads it like production does.
func saveTopUpBonusConfig(t *testing.T, settings *model.TopUpBonusSettings) {
	t.Helper()
	current, err := model.CurrentTopUpBonusSettings()
	require.NoError(t, err)
	data, err := common.Marshal(settings.Config)
	require.NoError(t, err)
	_, err = model.SaveTopUpBonusConfig(data, current.Version, 1, topUpBonusTestNow)
	require.NoError(t, err)
}

func TestQuoteTopUpKeepsThePriceAndFreezesTheBonus(t *testing.T) {
	setupTopUpBonusTestDB(t)
	settings := topUpBonusTestSettings(t, 4, func(c *model.TopUpBonusConfig) {
		c.Site.Mode = model.TopUpBonusModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpBonusModeOff, model.TopUpBonusModeOff, model.TopUpBonusModeShadow
		c.FixedRate = "0.0333"
	})
	saveTopUpBonusConfig(t, settings)

	quote, err := QuoteTopUp(7, 500001, decimal.RequireFromString("7.30"), "cny")
	require.NoError(t, err)
	assert.Equal(t, "CNY", quote.Currency)
	assert.Equal(t, "7.30", quote.PayAmount, "the bonus never changes the price")
	assert.Equal(t, 16650, quote.BonusQuota(), "floor(500001 × 0.0333) = floor(16650.03)")
	assert.Equal(t, 516651, quote.ExpectedQuota())
	assert.Equal(t, int64(1), quote.Grant.ConfigVersion)
	assert.Equal(t, 16650, quote.Grant.BonusQuota)
	assert.Contains(t, quote.Grant.Decision, `"shadow_rates"`, "administrators keep the shadow evidence")
	public, err := common.Marshal(quote.PublicView())
	require.NoError(t, err)
	assert.NotContains(t, string(public), "shadow")
	assert.JSONEq(t, `{"principal_quota":500001,"bonus_quota":16650,"expected_quota":516651,"currency":"CNY","pay_amount":"7.30",
		"applied_rate":"0.0333","amount_rate":"0","fixed_rate":"0.0333","site_rate":"0","personal_rate":"0","cap_applied":false}`, string(public))

	yen, err := QuoteTopUp(7, 500000, decimal.RequireFromString("1000.5"), "JPY")
	require.NoError(t, err)
	assert.Equal(t, "1001", yen.PayAmount, "zero-decimal currencies round half-up to whole units")

	saveTopUpBonusConfig(t, topUpBonusTestSettings(t, 5, func(c *model.TopUpBonusConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpBonusModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpBonusModeOff, model.TopUpBonusModeOff, model.TopUpBonusModeOff
	}))
	plain, err := QuoteTopUpBonus(7, 500000)
	require.NoError(t, err)
	assert.Zero(t, plain.BonusQuota())
	assert.Empty(t, plain.Grant.Decision, "an inactive configuration records nothing")
}

func TestTopUpQuoteExpectation(t *testing.T) {
	quote := &TopUpQuote{TopUpBonusQuote: &TopUpBonusQuote{PrincipalQuota: 1000, Grant: model.TopUpBonusGrant{BonusQuota: 50}}, Currency: "CNY", PayAmount: "10.00"}
	raw := func(v string) json.RawMessage { return json.RawMessage(v) }
	price := func(amount, currency string) TopUpQuoteExpectation {
		return TopUpQuoteExpectation{PayAmount: raw(amount), Currency: raw(currency)}
	}
	bonus := func(value string) TopUpQuoteExpectation {
		return TopUpQuoteExpectation{TopUpBonusExpectation: TopUpBonusExpectation{BonusQuota: raw(value)}}
	}
	for name, tc := range map[string]struct {
		expectation TopUpQuoteExpectation
		want        error
	}{
		"absent":            {TopUpQuoteExpectation{}, nil},
		"equal":             {price(`"10.00"`, `"CNY"`), nil},
		"trailing zeros":    {price(`"10"`, `"cny"`), nil},
		"changed amount":    {price(`"9.99"`, `"CNY"`), ErrTopUpQuoteChanged},
		"changed currency":  {price(`"10.00"`, `"USD"`), ErrTopUpQuoteChanged},
		"one field":         {TopUpQuoteExpectation{PayAmount: raw(`"10.00"`)}, ErrTopUpQuoteExpectationInvalid},
		"null":              {price(`null`, `null`), ErrTopUpQuoteExpectationInvalid},
		"number":            {price(`10`, `"CNY"`), ErrTopUpQuoteExpectationInvalid},
		"empty":             {price(`""`, `"CNY"`), ErrTopUpQuoteExpectationInvalid},
		"sub-cent":          {price(`"10.001"`, `"CNY"`), ErrTopUpQuoteExpectationInvalid},
		"negative":          {price(`"-10"`, `"CNY"`), ErrTopUpQuoteExpectationInvalid},
		"bonus equal":       {bonus(`50`), nil},
		"bonus changed":     {bonus(`49`), ErrTopUpQuoteChanged},
		"bonus zero":        {bonus(`0`), ErrTopUpQuoteChanged},
		"bonus null":        {bonus(`null`), ErrTopUpQuoteExpectationInvalid},
		"bonus string":      {bonus(`"50"`), ErrTopUpQuoteExpectationInvalid},
		"bonus negative":    {bonus(`-1`), ErrTopUpQuoteExpectationInvalid},
		"bonus decimal":     {bonus(`50.0`), ErrTopUpQuoteExpectationInvalid},
		"bonus exponent":    {bonus(`5e1`), ErrTopUpQuoteExpectationInvalid},
		"bonus above limit": {bonus(`999999999999999999`), ErrTopUpQuoteExpectationInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			err := tc.expectation.Check(quote)
			if tc.want == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tc.want)
			}
		})
	}
	// Remotely priced checkouts confirm the bonus alone and get their own code.
	assert.ErrorIs(t, TopUpBonusExpectation{BonusQuota: raw(`49`)}.Check(quote.TopUpBonusQuote), ErrTopUpBonusChanged)
	assert.NoError(t, TopUpBonusExpectation{}.Check(quote.TopUpBonusQuote))
}
