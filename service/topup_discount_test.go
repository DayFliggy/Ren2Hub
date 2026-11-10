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
var topUpDiscountTestNow = time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

const topUpDiscountTestCutoff = int64(1793289600)

func topUpDiscountTiers(pairs ...string) []model.TopUpDiscountTierConfig {
	tiers := []model.TopUpDiscountTierConfig{}
	for _, pair := range pairs {
		threshold, rate, _ := strings.Cut(pair, ":")
		tiers = append(tiers, model.TopUpDiscountTierConfig{ThresholdQuota: threshold, Rate: rate})
	}
	return tiers
}

// topUpDiscountTestSettings: fixed 1%, cap 5%, site 1000→1% / 5000→2%,
// day 100→3%, week 500→4%, month 1000→5%, all on, user 1 excluded.
func topUpDiscountTestSettings(t *testing.T, version int64, mutate func(*model.TopUpDiscountConfig)) *model.TopUpDiscountSettings {
	t.Helper()
	config := model.DefaultTopUpDiscountConfig()
	config.FixedRate = "0.01"
	config.Site = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("1000:0.01", "5000:0.02")}
	config.Personal.Day = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("100:0.03")}
	config.Personal.Week = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("500:0.04")}
	config.Personal.Month = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("1000:0.05")}
	if mutate != nil {
		mutate(&config)
	}
	settings, err := model.BuildTopUpDiscountSettings(config)
	require.NoError(t, err)
	settings.Version = version
	return settings
}

func setupTopUpDiscountTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.TopUpDiscountUsageHourly{}, &model.TopUpDiscountConfigVersion{}))
	previousDB, previousDay := model.DB, currentTopUpDiscountDay.Load()
	model.DB = db
	ResetTopUpDiscountCaches()
	t.Cleanup(func() {
		model.DB = previousDB
		ResetTopUpDiscountCaches()
		currentTopUpDiscountDay.Store(previousDay)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func seedTopUpDiscountRows(t *testing.T, db *gorm.DB, rows ...model.TopUpDiscountUsageHourly) {
	t.Helper()
	for _, row := range rows {
		require.NoError(t, db.Create(&row).Error)
	}
}

func i64(v int64) *int64 { return &v }

func windows(day, week, month int64) *model.TopUpDiscountWindowSums {
	return &model.TopUpDiscountWindowSums{DayQuota: day, WeekQuota: week, MonthQuota: month}
}

func TestEvaluateTopUpDiscountFormula(t *testing.T) {
	settings := topUpDiscountTestSettings(t, 1, nil)
	decision := evaluateTopUpDiscount(settings, topUpDiscountAmounts{Site: i64(5000), Personal: windows(100, 500, 1000)})
	assert.Equal(t, "0.08", decision.RawRate.String(), "0.01 + 0.02 + max(0.03, 0.04, 0.05)")
	assert.Equal(t, "0.05", decision.AppliedRate.String())
	assert.True(t, decision.CapApplied)
	assert.Equal(t, "0.95", decision.Factor.String())
	assert.Equal(t, topUpDiscountWindowMonth, decision.WinnerWindow)

	shadow := topUpDiscountTestSettings(t, 1, func(c *model.TopUpDiscountConfig) {
		c.Site.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeOff, model.TopUpDiscountModeShadow
	})
	decision = evaluateTopUpDiscount(shadow, topUpDiscountAmounts{Site: i64(5000), Personal: windows(100, 500, 1000)})
	assert.Equal(t, "0.05", decision.RawRate.String(), "shadow month and off site contribute 0")
	assert.False(t, decision.CapApplied)
	assert.Equal(t, topUpDiscountWindowWeek, decision.WinnerWindow)
	assert.Equal(t, map[string]string{topUpDiscountWindowMonth: "0.05"}, decision.ShadowRates)

	below := evaluateTopUpDiscount(settings, topUpDiscountAmounts{Site: i64(999), Personal: windows(99, 499, 999)})
	assert.Equal(t, "0.01", below.AppliedRate.String(), "below every first tier only fixed applies")
	assert.Empty(t, below.WinnerWindow)
	assert.Equal(t, below.AppliedRate, evaluateTopUpDiscount(settings, topUpDiscountAmounts{}).AppliedRate, "unread sources contribute 0")

	tie := topUpDiscountTestSettings(t, 1, func(c *model.TopUpDiscountConfig) {
		c.Personal.Day.Tiers = topUpDiscountTiers("100:0.05")
	})
	assert.Equal(t, topUpDiscountWindowMonth, evaluateTopUpDiscount(tie, topUpDiscountAmounts{Personal: windows(100, 0, 1000)}).WinnerWindow,
		"ties go to the longer window")

	// The amount tier is keyed on the order's credited quota and joins the sum.
	amount := topUpDiscountTestSettings(t, 1, func(c *model.TopUpDiscountConfig) {
		c.CapRate = "0.1"
		c.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("1000:0.02", "5000:0.03")}
	})
	decision = evaluateTopUpDiscount(amount, topUpDiscountAmounts{Order: 4999, Personal: windows(100, 0, 0)})
	assert.Equal(t, "0.02", decision.AmountRate.String())
	assert.Equal(t, "0.06", decision.AppliedRate.String(), "amount 0.02 + fixed 0.01 + day 0.03")
	assert.Equal(t, "0.03", evaluateTopUpDiscount(amount, topUpDiscountAmounts{Order: 5000}).AmountRate.String())
}

func seedTopUpDiscountResolveRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	cutoff := topUpDiscountTestCutoff
	seedTopUpDiscountRows(t, db,
		model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: cutoff, Quota: 1_000_000}, // today never counts
		model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: cutoff - 3600, Quota: 100},
		model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: cutoff - 3*86400, Quota: 400},
		model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: cutoff - 20*86400, Quota: 500},
		model.TopUpDiscountUsageHourly{UserID: 8, HourUTC: cutoff - 3600, Quota: -5000}, // refunds floor at 0
		model.TopUpDiscountUsageHourly{UserID: 9, HourUTC: cutoff - 3600, Quota: 3000},
		model.TopUpDiscountUsageHourly{UserID: 1, HourUTC: cutoff - 3600, Quota: 1000}, // excluded from the site total
	)
}

func mustResolve(t *testing.T, settings *model.TopUpDiscountSettings, userID int, order int64, now time.Time) topUpDiscountResolution {
	t.Helper()
	result, err := resolveTopUpDiscount(settings, userID, order, now)
	require.NoError(t, err)
	return result
}

func TestResolveTopUpDiscountFromHourlyWindows(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	seedTopUpDiscountResolveRows(t, db)
	settings := topUpDiscountTestSettings(t, 1, nil)

	// Site total = 1000 (user 7) + 0 (user 8 floored) + 3000 (user 9) = 4000 → 1%.
	// Counting user 1 would reach 5000 → 2%; counting user 8 raw would drop below 1000.
	user7 := mustResolve(t, settings, 7, 0, topUpDiscountTestNow)
	assert.Equal(t, windows(100, 500, 1000), user7.Personal)
	assert.Equal(t, TopUpDiscountOK, user7.Decision.Status)
	assert.Equal(t, "0.01", user7.Decision.SiteRate.String())
	assert.Equal(t, "0.07", user7.Decision.RawRate.String())
	assert.Equal(t, "0.95", user7.Decision.Factor.String())
	assert.Equal(t, "2026-10-30", user7.Decision.BusinessDate)

	user8 := mustResolve(t, settings, 8, 0, topUpDiscountTestNow)
	assert.Equal(t, "0.02", user8.Decision.AppliedRate.String(), "a negative window floors at 0")

	amount := topUpDiscountTestSettings(t, 2, func(c *model.TopUpDiscountConfig) {
		c.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("1000:0.02")}
	})
	excluded := mustResolve(t, amount, 1, 1000, topUpDiscountTestNow)
	assert.Equal(t, "0.03", excluded.Decision.AppliedRate.String(), "excluded users keep the fixed and amount rates")
	assert.Equal(t, topUpDiscountReasonExcluded, excluded.Decision.Reason)
	assert.Nil(t, excluded.Personal)

	inactive := topUpDiscountTestSettings(t, 3, func(c *model.TopUpDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeOff, model.TopUpDiscountModeOff, model.TopUpDiscountModeOff
	})
	decision := mustResolve(t, inactive, 7, 0, topUpDiscountTestNow).Decision
	assert.Equal(t, TopUpDiscountInactive, decision.Status)
	assert.Equal(t, "1", decision.Factor.String())
}

// A table that changes the price must be read; a shadow-only source may fail
// without blocking the live components.
func TestResolveTopUpDiscountFailureRules(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	require.NoError(t, db.Migrator().DropTable(&model.TopUpDiscountUsageHourly{}))
	before := model.GetTopUpDiscountHealth().WindowQueryFailures.Count

	_, err := resolveTopUpDiscount(topUpDiscountTestSettings(t, 1, nil), 7, 0, topUpDiscountTestNow)
	assert.ErrorIs(t, err, ErrTopUpDiscountUnavailable, "a live site table cannot be skipped")
	assert.Greater(t, model.GetTopUpDiscountHealth().WindowQueryFailures.Count, before)

	personalOn := topUpDiscountTestSettings(t, 2, func(c *model.TopUpDiscountConfig) { c.Site.Mode = model.TopUpDiscountModeShadow })
	_, err = resolveTopUpDiscount(personalOn, 7, 0, topUpDiscountTestNow.Add(topUpDiscountSiteRetryAfter))
	assert.ErrorIs(t, err, ErrTopUpDiscountUnavailable, "a live personal window cannot be skipped")

	shadowOnly := topUpDiscountTestSettings(t, 3, func(c *model.TopUpDiscountConfig) {
		c.Site.Mode = model.TopUpDiscountModeShadow
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeShadow, model.TopUpDiscountModeOff, model.TopUpDiscountModeOff
		c.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("10:0.02")}
	})
	decision := mustResolve(t, shadowOnly, 7, 10, topUpDiscountTestNow.Add(2*topUpDiscountSiteRetryAfter)).Decision
	assert.Equal(t, TopUpDiscountShadowUnavailable, decision.Status)
	assert.Equal(t, "site,personal", decision.Reason)
	assert.Equal(t, "0.97", decision.Factor.String(), "fixed and amount still apply")

	excluded := mustResolve(t, topUpDiscountTestSettings(t, 4, nil), 1, 0, topUpDiscountTestNow)
	assert.Equal(t, TopUpDiscountOK, excluded.Decision.Status, "excluded users never read usage")

	require.NoError(t, db.AutoMigrate(&model.TopUpDiscountUsageHourly{}))
	failures := model.GetTopUpDiscountHealth().WindowQueryFailures.Count
	_, err = resolveTopUpDiscount(topUpDiscountTestSettings(t, 1, nil), 7, 0, topUpDiscountTestNow.Add(2*topUpDiscountSiteRetryAfter+time.Second))
	assert.ErrorIs(t, err, ErrTopUpDiscountUnavailable, "the site query backs off briefly")
	assert.Equal(t, failures, model.GetTopUpDiscountHealth().WindowQueryFailures.Count, "no query runs during the backoff")
	retried := mustResolve(t, topUpDiscountTestSettings(t, 1, nil), 7, 0, topUpDiscountTestNow.Add(3*topUpDiscountSiteRetryAfter+time.Second))
	assert.Equal(t, TopUpDiscountOK, retried.Decision.Status, "a failed read is retried, not cached")
}

func TestTopUpDiscountDayIsStableAndRollsOver(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	seedTopUpDiscountResolveRows(t, db)
	settings := topUpDiscountTestSettings(t, 1, nil)
	first := mustResolve(t, settings, 7, 0, topUpDiscountTestNow).Personal

	// A write during the business day lands at or after the cutoff.
	seedTopUpDiscountRows(t, db, model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: topUpDiscountTestCutoff + 3600, Quota: 900})
	assert.Equal(t, first, mustResolve(t, settings, 7, 0, topUpDiscountTestNow.Add(5*time.Hour)).Personal)
	currentTopUpDiscountDay.Store(nil)
	assert.Equal(t, first, mustResolve(t, settings, 7, 0, topUpDiscountTestNow).Personal,
		"recomputing after a restart gives the same windows")

	// The next business day counts yesterday's rows, including the cutoff hour.
	next := mustResolve(t, settings, 7, 0, topUpDiscountTestNow.Add(24*time.Hour))
	assert.Equal(t, "2026-10-31", next.Decision.BusinessDate)
	assert.Equal(t, int64(1_000_900), next.Personal.DayQuota)
	assert.Equal(t, int64(1_001_900), next.Personal.MonthQuota)

	utc := topUpDiscountTestSettings(t, 2, func(c *model.TopUpDiscountConfig) { c.Timezone = "UTC" })
	mustResolve(t, utc, 7, 0, topUpDiscountTestNow.Add(24*time.Hour))
	assert.Equal(t, "UTC", currentTopUpDiscountDay.Load().timezone, "a timezone change replaces the day")
}

func TestTopUpDiscountSiteTotalFollowsConfigVersion(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	seedTopUpDiscountResolveRows(t, db)
	excluding := topUpDiscountTestSettings(t, 1, nil)
	including := topUpDiscountTestSettings(t, 2, func(c *model.TopUpDiscountConfig) { c.ExcludedUserIDs = []int{} })
	assert.Equal(t, "0.01", mustResolve(t, excluding, 9, 0, topUpDiscountTestNow).Decision.SiteRate.String(), "4000 without user 1")
	assert.Equal(t, "0.02", mustResolve(t, including, 9, 0, topUpDiscountTestNow).Decision.SiteRate.String(), "5000 with user 1")
}

func TestResolveTopUpDiscountConcurrentAcrossMidnight(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	seedTopUpDiscountResolveRows(t, db)
	settings := topUpDiscountTestSettings(t, 1, nil)
	midnight := time.Unix(topUpDiscountTestCutoff+86400, 0)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now, want := midnight.Add(-time.Second), "2026-10-30"
			if i%2 == 1 {
				now, want = midnight, "2026-10-31"
			}
			result, err := resolveTopUpDiscount(settings, 7+i%3, 0, now)
			assert.NoError(t, err)
			assert.Equal(t, want, result.Decision.BusinessDate)
		}(i)
	}
	wg.Wait()
	assert.Equal(t, "2026-10-31", currentTopUpDiscountDay.Load().date, "the shared day never moves backwards")
}

// Concurrent first reads of one user share a single query, so a write that
// commits between two queries cannot give requests of one day different sums.
func TestTopUpDiscountConcurrentFirstReadsShareOneQuery(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	seedTopUpDiscountRows(t, db, model.TopUpDiscountUsageHourly{UserID: 7, HourUTC: topUpDiscountTestCutoff - 3600, Quota: 700})
	var queries atomic.Int32
	release := make(chan struct{})
	require.NoError(t, db.Callback().Row().Before("gorm:row").Register("test:count_window_sums", func(tx *gorm.DB) {
		if tx.Statement.Table == "topup_discount_usage_hourly" && queries.Add(1) == 1 {
			<-release
		}
	}))
	day, err := newTopUpDiscountDay(topUpDiscountTestSettings(t, 1, nil).Location, topUpDiscountTestNow)
	require.NoError(t, err)

	const readers = 20
	results := make([]model.TopUpDiscountWindowSums, readers)
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
		assert.Equal(t, model.TopUpDiscountWindowSums{DayQuota: 700, WeekQuota: 700, MonthQuota: 700}, sums)
	}
}

func TestTopUpDiscountSelfViewHidesSiteTotalAndShadow(t *testing.T) {
	db := setupTopUpDiscountTestDB(t)
	seedTopUpDiscountResolveRows(t, db)
	settings := topUpDiscountTestSettings(t, 1, func(c *model.TopUpDiscountConfig) {
		c.Personal.Day.Mode = model.TopUpDiscountModeShadow
		c.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("0:0.01", "1000:0.02")}
	})
	view, err := topUpDiscountSelfView(settings, 7, topUpDiscountTestNow)
	require.NoError(t, err)
	assert.Equal(t, TopUpDiscountOK, view.Status)
	assert.Equal(t, "0.01", view.FixedRate)
	assert.Equal(t, "0.01", view.SiteRate)
	assert.Equal(t, "0.05", view.PersonalRate)
	assert.Equal(t, "0.07", view.RawRate, "amount tiers are listed, not applied, even from a 0 threshold")
	assert.Equal(t, "0.95", view.Factor)
	assert.Equal(t, topUpDiscountTiers("0:0.01", "1000:0.02"), view.AmountTiers)
	require.Len(t, view.Windows, 2, "the shadow day window is not shown")
	assert.Equal(t, TopUpDiscountWindowView{Window: topUpDiscountWindowWeek, Quota: 500, Rate: "0.04", Tiers: topUpDiscountTiers("500:0.04")}, view.Windows[0])
	assert.Equal(t, int64(1000), view.Windows[1].Quota)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "4000", "the site total stays private")
	assert.NotContains(t, string(encoded), "shadow")

	shadowOnly := topUpDiscountTestSettings(t, 2, func(c *model.TopUpDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpDiscountModeShadow
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeShadow, model.TopUpDiscountModeShadow, model.TopUpDiscountModeShadow
	})
	view, err = topUpDiscountSelfView(shadowOnly, 7, topUpDiscountTestNow)
	require.NoError(t, err)
	assert.Equal(t, TopUpDiscountInactive, view.Status)
	assert.Equal(t, "1", view.Factor)
	assert.Empty(t, view.Windows)

	amountOnly := topUpDiscountTestSettings(t, 3, func(c *model.TopUpDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeOff, model.TopUpDiscountModeOff, model.TopUpDiscountModeOff
		c.Amount = model.TopUpDiscountTableConfig{Mode: model.TopUpDiscountModeOn, Tiers: topUpDiscountTiers("0:0.02")}
	})
	view, err = topUpDiscountSelfView(amountOnly, 7, topUpDiscountTestNow)
	require.NoError(t, err)
	assert.Equal(t, "1", view.Factor, "the amount tier depends on the order and is never folded in")
	assert.Equal(t, topUpDiscountTiers("0:0.02"), view.AmountTiers)

	excluded, err := topUpDiscountSelfView(settings, 1, topUpDiscountTestNow)
	require.NoError(t, err)
	assert.Equal(t, "0.99", excluded.Factor, "fixed only; the 0-threshold amount tier is listed")
	assert.Empty(t, excluded.Windows, "excluded users have no personal windows")
}

// saveTopUpDiscountConfig stores settings as the next version through the
// normal save path, so QuoteTopUp reads it like production does.
func saveTopUpDiscountConfig(t *testing.T, settings *model.TopUpDiscountSettings) {
	t.Helper()
	current, err := model.CurrentTopUpDiscountSettings()
	require.NoError(t, err)
	data, err := common.Marshal(settings.Config)
	require.NoError(t, err)
	_, err = model.SaveTopUpDiscountConfig(data, current.Version, 1, topUpDiscountTestNow)
	require.NoError(t, err)
}

func TestQuoteTopUpRoundsOnceAndFreezesTheDecision(t *testing.T) {
	setupTopUpDiscountTestDB(t)
	settings := topUpDiscountTestSettings(t, 4, func(c *model.TopUpDiscountConfig) {
		c.Site.Mode = model.TopUpDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeOff, model.TopUpDiscountModeOff, model.TopUpDiscountModeShadow
		c.FixedRate = "0.0333"
	})
	saveTopUpDiscountConfig(t, settings)

	quote, err := QuoteTopUp(7, 500000, decimal.RequireFromString("7.30"), "cny")
	require.NoError(t, err)
	assert.Equal(t, "CNY", quote.Currency)
	assert.Equal(t, "7.06", quote.PayAmount, "7.30 × 0.9667 = 7.05691 → half-up 7.06")
	record, err := quote.Record()
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, int64(1), record.ConfigVersion)
	assert.Equal(t, "7.3", record.ListPayAmount)
	assert.Contains(t, record.Decision, `"shadow_rates"`, "administrators keep the shadow evidence")
	public, err := common.Marshal(quote.PublicView())
	require.NoError(t, err)
	assert.NotContains(t, string(public), "shadow")
	assert.Contains(t, string(public), `"list_pay_amount":"7.30"`)

	yen, err := QuoteTopUp(7, 500000, decimal.RequireFromString("1000.5"), "JPY")
	require.NoError(t, err)
	assert.Equal(t, "967", yen.PayAmount, "zero-decimal currencies round to whole units")

	saveTopUpDiscountConfig(t, topUpDiscountTestSettings(t, 5, func(c *model.TopUpDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.TopUpDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.TopUpDiscountModeOff, model.TopUpDiscountModeOff, model.TopUpDiscountModeOff
	}))
	plain, err := QuoteTopUp(7, 500000, decimal.RequireFromString("7.3"), "CNY")
	require.NoError(t, err)
	assert.Equal(t, "7.30", plain.PayAmount)
	record, err = plain.Record()
	require.NoError(t, err)
	assert.Nil(t, record, "an inactive configuration stores nothing")
}

func TestTopUpQuoteExpectation(t *testing.T) {
	quote := &TopUpQuote{Currency: "CNY", PayAmount: "10.00"}
	raw := func(v string) json.RawMessage { return json.RawMessage(v) }
	for name, tc := range map[string]struct {
		expectation TopUpQuoteExpectation
		want        error
	}{
		"absent":           {TopUpQuoteExpectation{}, nil},
		"equal":            {TopUpQuoteExpectation{raw(`"10.00"`), raw(`"CNY"`)}, nil},
		"trailing zeros":   {TopUpQuoteExpectation{raw(`"10"`), raw(`"cny"`)}, nil},
		"changed amount":   {TopUpQuoteExpectation{raw(`"9.99"`), raw(`"CNY"`)}, ErrTopUpQuoteChanged},
		"changed currency": {TopUpQuoteExpectation{raw(`"10.00"`), raw(`"USD"`)}, ErrTopUpQuoteChanged},
		"one field":        {TopUpQuoteExpectation{PayAmount: raw(`"10.00"`)}, ErrTopUpQuoteExpectationInvalid},
		"null":             {TopUpQuoteExpectation{raw(`null`), raw(`null`)}, ErrTopUpQuoteExpectationInvalid},
		"number":           {TopUpQuoteExpectation{raw(`10`), raw(`"CNY"`)}, ErrTopUpQuoteExpectationInvalid},
		"empty":            {TopUpQuoteExpectation{raw(`""`), raw(`"CNY"`)}, ErrTopUpQuoteExpectationInvalid},
		"sub-cent":         {TopUpQuoteExpectation{raw(`"10.001"`), raw(`"CNY"`)}, ErrTopUpQuoteExpectationInvalid},
		"negative":         {TopUpQuoteExpectation{raw(`"-10"`), raw(`"CNY"`)}, ErrTopUpQuoteExpectationInvalid},
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
}
