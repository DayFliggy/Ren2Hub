package service

import (
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Thursday 2026-10-30 18:00 in Asia/Shanghai; the cutoff is 2026-10-29T16:00Z.
var usageDiscountTestNow = time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)

const usageDiscountTestCutoff = int64(1793289600)

func usageDiscountTiers(pairs ...string) []model.UsageDiscountTierConfig {
	tiers := []model.UsageDiscountTierConfig{}
	for _, pair := range pairs {
		threshold, rate, _ := strings.Cut(pair, ":")
		tiers = append(tiers, model.UsageDiscountTierConfig{ThresholdQuota: threshold, Rate: rate})
	}
	return tiers
}

// usageDiscountTestSettings: fixed 1%, cap 5%, site 1000→1% / 5000→2%,
// day 100→3%, week 500→4%, month 1000→5%, all on, user 1 excluded.
func usageDiscountTestSettings(t *testing.T, version int64, mutate func(*model.UsageDiscountConfig)) *model.UsageDiscountSettings {
	t.Helper()
	config := model.DefaultUsageDiscountConfig()
	config.FixedRate = "0.01"
	config.Site = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeOn, Tiers: usageDiscountTiers("1000:0.01", "5000:0.02")}
	config.Personal.Day = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeOn, Tiers: usageDiscountTiers("100:0.03")}
	config.Personal.Week = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeOn, Tiers: usageDiscountTiers("500:0.04")}
	config.Personal.Month = model.UsageDiscountTableConfig{Mode: model.UsageDiscountModeOn, Tiers: usageDiscountTiers("1000:0.05")}
	if mutate != nil {
		mutate(&config)
	}
	settings, err := model.BuildUsageDiscountSettings(config)
	require.NoError(t, err)
	settings.Version = version
	return settings
}

func setupUsageDiscountTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.UsageDiscountHourly{}))
	previousDB, previousDay := model.DB, currentUsageDiscountDay.Load()
	model.DB = db
	currentUsageDiscountDay.Store(nil)
	t.Cleanup(func() {
		model.DB = previousDB
		currentUsageDiscountDay.Store(previousDay)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func seedUsageDiscountRows(t *testing.T, db *gorm.DB, rows ...model.UsageDiscountHourly) {
	t.Helper()
	for _, row := range rows {
		require.NoError(t, db.Create(&row).Error)
	}
}

func TestEvaluateUsageDiscountFormula(t *testing.T) {
	settings := usageDiscountTestSettings(t, 1, nil)
	decision := evaluateUsageDiscount(settings, &usageDiscountAmounts{Site: 5000, Day: 100, Week: 500, Month: 1000})
	assert.Equal(t, "0.08", decision.RawRate.String(), "0.01 + 0.02 + max(0.03, 0.04, 0.05)")
	assert.Equal(t, "0.05", decision.AppliedRate.String())
	assert.True(t, decision.CapApplied)
	assert.Equal(t, "0.95", decision.Factor.String())
	assert.Equal(t, usageDiscountWindowMonth, decision.WinnerWindow)

	shadow := usageDiscountTestSettings(t, 1, func(c *model.UsageDiscountConfig) {
		c.Site.Mode, c.Personal.Month.Mode = model.UsageDiscountModeOff, model.UsageDiscountModeShadow
	})
	decision = evaluateUsageDiscount(shadow, &usageDiscountAmounts{Site: 5000, Day: 100, Week: 500, Month: 1000})
	assert.Equal(t, "0.05", decision.RawRate.String(), "shadow month and off site contribute 0")
	assert.False(t, decision.CapApplied)
	assert.Equal(t, usageDiscountWindowWeek, decision.WinnerWindow)
	assert.Equal(t, map[string]string{usageDiscountWindowMonth: "0.05"}, decision.ShadowRates)

	below := evaluateUsageDiscount(settings, &usageDiscountAmounts{Site: 999, Day: 99, Week: 499, Month: 999})
	assert.Equal(t, "0.01", below.AppliedRate.String(), "below every first tier only fixed applies")
	assert.Empty(t, below.WinnerWindow)
	assert.Equal(t, below.AppliedRate, evaluateUsageDiscount(settings, nil).AppliedRate)

	tie := usageDiscountTestSettings(t, 1, func(c *model.UsageDiscountConfig) {
		c.Personal.Day.Tiers = usageDiscountTiers("100:0.05")
	})
	assert.Equal(t, usageDiscountWindowMonth, evaluateUsageDiscount(tie, &usageDiscountAmounts{Day: 100, Month: 1000}).WinnerWindow,
		"ties go to the longer window")
}

func seedUsageDiscountResolveRows(t *testing.T, db *gorm.DB) {
	t.Helper()
	cutoff := usageDiscountTestCutoff
	seedUsageDiscountRows(t, db,
		model.UsageDiscountHourly{UserID: 7, HourUTC: cutoff, Quota: 1_000_000}, // today never counts
		model.UsageDiscountHourly{UserID: 7, HourUTC: cutoff - 3600, Quota: 100},
		model.UsageDiscountHourly{UserID: 7, HourUTC: cutoff - 3*86400, Quota: 400},
		model.UsageDiscountHourly{UserID: 7, HourUTC: cutoff - 20*86400, Quota: 500},
		model.UsageDiscountHourly{UserID: 8, HourUTC: cutoff - 3600, Quota: -5000}, // refunds floor at 0
		model.UsageDiscountHourly{UserID: 9, HourUTC: cutoff - 3600, Quota: 3000},
		model.UsageDiscountHourly{UserID: 1, HourUTC: cutoff - 3600, Quota: 1000}, // excluded from the site total
	)
}

func TestResolveUsageDiscountFromHourlyWindows(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	seedUsageDiscountResolveRows(t, db)
	settings := usageDiscountTestSettings(t, 1, nil)

	// Site total = 1000 (user 7) + 0 (user 8 floored) + 3000 (user 9) = 4000 → 1%.
	// Counting user 1 would reach 5000 → 2%; counting user 8 raw would drop below 1000.
	user7 := resolveUsageDiscount(settings, 7, false, usageDiscountTestNow)
	assert.Equal(t, usageDiscountAmounts{Site: 4000, Day: 100, Week: 500, Month: 1000}, user7.Amounts)
	assert.Equal(t, hosttypes.UsageDiscountOK, user7.Decision.Status)
	assert.Equal(t, "0.01", user7.Decision.SiteRate.String())
	assert.Equal(t, "0.07", user7.Decision.RawRate.String())
	assert.Equal(t, "0.95", user7.Decision.Factor.String())
	assert.Equal(t, "2026-10-30", user7.Decision.BusinessDate)

	user8 := resolveUsageDiscount(settings, 8, false, usageDiscountTestNow)
	assert.Equal(t, usageDiscountAmounts{Site: 4000}, user8.Amounts)
	assert.Equal(t, "0.02", user8.Decision.AppliedRate.String())

	excluded := resolveUsageDiscount(settings, 1, false, usageDiscountTestNow)
	assert.Equal(t, "0.01", excluded.Decision.AppliedRate.String(), "excluded users keep only the fixed rate")
	assert.Equal(t, usageDiscountReasonExcluded, excluded.Decision.Reason)
	assert.False(t, excluded.WindowsRead)

	channelTest := resolveUsageDiscount(settings, 7, true, usageDiscountTestNow).Decision
	assert.Equal(t, hosttypes.UsageDiscountNotApplicable, channelTest.Status)
	assert.Equal(t, "1", channelTest.Factor.String())

	inactive := usageDiscountTestSettings(t, 2, func(c *model.UsageDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.UsageDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.UsageDiscountModeOff, model.UsageDiscountModeOff, model.UsageDiscountModeOff
	})
	decision := resolveUsageDiscount(inactive, 7, false, usageDiscountTestNow).Decision
	assert.Equal(t, hosttypes.UsageDiscountInactive, decision.Status)
	assert.Equal(t, "1", decision.Factor.String())
}

func TestResolveUsageDiscountUnavailableKeepsFixedRateAndRetries(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	settings := usageDiscountTestSettings(t, 1, nil)
	require.NoError(t, db.Migrator().DropTable(&model.UsageDiscountHourly{}))
	before := model.GetUsageDiscountHealth().WindowQueryFailures.Count

	decision := resolveUsageDiscount(settings, 7, false, usageDiscountTestNow).Decision
	assert.Equal(t, hosttypes.UsageDiscountUnavailable, decision.Status)
	assert.Equal(t, "usage_data_unavailable", decision.Reason)
	assert.Equal(t, "0.99", decision.Factor.String(), "the fixed rate still applies")
	assert.Greater(t, model.GetUsageDiscountHealth().WindowQueryFailures.Count, before)

	require.NoError(t, db.AutoMigrate(&model.UsageDiscountHourly{}))
	failures := model.GetUsageDiscountHealth().WindowQueryFailures.Count
	soon := resolveUsageDiscount(settings, 7, false, usageDiscountTestNow.Add(time.Second)).Decision
	assert.Equal(t, hosttypes.UsageDiscountUnavailable, soon.Status, "the site query backs off briefly")
	assert.Equal(t, failures, model.GetUsageDiscountHealth().WindowQueryFailures.Count, "no query runs during the backoff")
	retried := resolveUsageDiscount(settings, 7, false, usageDiscountTestNow.Add(usageDiscountSiteRetryAfter)).Decision
	assert.Equal(t, hosttypes.UsageDiscountOK, retried.Status, "a failed read is retried, not cached")
}

func TestUsageDiscountDayIsStableAndRollsOver(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	seedUsageDiscountResolveRows(t, db)
	settings := usageDiscountTestSettings(t, 1, nil)
	first := resolveUsageDiscount(settings, 7, false, usageDiscountTestNow).Amounts

	// A write during the business day lands at or after the cutoff.
	seedUsageDiscountRows(t, db, model.UsageDiscountHourly{UserID: 7, HourUTC: usageDiscountTestCutoff + 3600, Quota: 900})
	assert.Equal(t, first, resolveUsageDiscount(settings, 7, false, usageDiscountTestNow.Add(5*time.Hour)).Amounts)
	currentUsageDiscountDay.Store(nil)
	assert.Equal(t, first, resolveUsageDiscount(settings, 7, false, usageDiscountTestNow).Amounts,
		"recomputing after a restart gives the same windows")

	// The next business day counts yesterday's rows, including the cutoff hour.
	next := resolveUsageDiscount(settings, 7, false, usageDiscountTestNow.Add(24*time.Hour))
	assert.Equal(t, "2026-10-31", next.Decision.BusinessDate)
	assert.Equal(t, int64(1_000_900), next.Amounts.Day)
	assert.Equal(t, int64(1_001_900), next.Amounts.Month)

	utc := usageDiscountTestSettings(t, 2, func(c *model.UsageDiscountConfig) { c.Timezone = "UTC" })
	resolveUsageDiscount(utc, 7, false, usageDiscountTestNow.Add(24*time.Hour))
	assert.Equal(t, "UTC", currentUsageDiscountDay.Load().timezone, "a timezone change replaces the day")
}

func TestUsageDiscountSiteTotalFollowsConfigVersion(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	seedUsageDiscountResolveRows(t, db)
	excluding := usageDiscountTestSettings(t, 1, nil)
	including := usageDiscountTestSettings(t, 2, func(c *model.UsageDiscountConfig) { c.ExcludedUserIDs = []int{} })
	assert.Equal(t, int64(4000), resolveUsageDiscount(excluding, 7, false, usageDiscountTestNow).Amounts.Site)
	assert.Equal(t, int64(5000), resolveUsageDiscount(including, 7, false, usageDiscountTestNow).Amounts.Site)
}

func TestResolveUsageDiscountConcurrentAcrossMidnight(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	seedUsageDiscountResolveRows(t, db)
	settings := usageDiscountTestSettings(t, 1, nil)
	midnight := time.Unix(usageDiscountTestCutoff+86400, 0)
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			now, want := midnight.Add(-time.Second), "2026-10-30"
			if i%2 == 1 {
				now, want = midnight, "2026-10-31"
			}
			assert.Equal(t, want, resolveUsageDiscount(settings, 7+i%3, false, now).Decision.BusinessDate)
		}(i)
	}
	wg.Wait()
	assert.Equal(t, "2026-10-31", currentUsageDiscountDay.Load().date, "the shared day never moves backwards")
}

func TestUsageDiscountSelfViewHidesSiteTotalAndShadow(t *testing.T) {
	db := setupUsageDiscountTestDB(t)
	seedUsageDiscountResolveRows(t, db)
	settings := usageDiscountTestSettings(t, 1, func(c *model.UsageDiscountConfig) { c.Personal.Day.Mode = model.UsageDiscountModeShadow })
	view := usageDiscountSelfView(settings, 7, usageDiscountTestNow)
	assert.Equal(t, hosttypes.UsageDiscountOK, view.Status)
	assert.Equal(t, "0.98", view.GlobalFactor, "1 - min(fixed 0.01 + site 0.01, cap)")
	assert.Equal(t, "0.95", view.Factor)
	require.Len(t, view.Windows, 2, "the shadow day window is not shown")
	assert.Equal(t, UsageDiscountWindowView{Window: usageDiscountWindowWeek, Quota: 500, Rate: "0.04", Tiers: usageDiscountTiers("500:0.04")}, view.Windows[0])
	assert.Equal(t, int64(1000), view.Windows[1].Quota)
	encoded, err := common.Marshal(view)
	require.NoError(t, err)
	assert.NotContains(t, string(encoded), "4000", "the site total stays private")
	assert.NotContains(t, string(encoded), "shadow")

	shadowOnly := usageDiscountTestSettings(t, 2, func(c *model.UsageDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.UsageDiscountModeShadow
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.UsageDiscountModeShadow, model.UsageDiscountModeShadow, model.UsageDiscountModeShadow
	})
	view = usageDiscountSelfView(shadowOnly, 7, usageDiscountTestNow)
	assert.Equal(t, hosttypes.UsageDiscountInactive, view.Status)
	assert.Equal(t, "1", view.Factor)
	assert.Empty(t, view.Windows)

	excluded := usageDiscountSelfView(settings, 1, usageDiscountTestNow)
	assert.Equal(t, "0.99", excluded.Factor)
	assert.Empty(t, excluded.Windows, "excluded users have no personal windows")
}

func TestUsageDiscountLogProjection(t *testing.T) {
	settings := usageDiscountTestSettings(t, 3, nil)
	decision := evaluateUsageDiscount(settings, &usageDiscountAmounts{Site: 5000, Month: 1000})
	other := model.NewLogOther()
	appendUsageDiscountInfo(other, hosttypes.ChannelRatioInfo{ChannelRatio: 1.9, BaseChannelRatio: 2, UsageDiscount: decision})
	snapshot := other.Snapshot()
	public, ok := snapshot["usage_discount"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "0.95", public["factor"])
	assert.Equal(t, "2", public["base_channel_ratio"])
	assert.Equal(t, true, public["cap_applied"])
	assert.NotContains(t, public, "raw_rate")
	admin := snapshot["admin_info"].(map[string]any)["usage_discount"].(map[string]any)
	assert.Equal(t, "0.08", admin["raw_rate"])
	assert.Equal(t, int64(3), admin["config_version"])

	shadowOnly := usageDiscountTestSettings(t, 4, func(c *model.UsageDiscountConfig) {
		c.FixedRate, c.Site.Mode = "0", model.UsageDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.UsageDiscountModeOff, model.UsageDiscountModeOff, model.UsageDiscountModeShadow
	})
	other = model.NewLogOther()
	appendUsageDiscountInfo(other, hosttypes.ChannelRatioInfo{ChannelRatio: 2, BaseChannelRatio: 2, UsageDiscount: evaluateUsageDiscount(shadowOnly, &usageDiscountAmounts{Month: 1000})})
	snapshot = other.Snapshot()
	assert.NotContains(t, snapshot, "usage_discount", "shadow-only decisions are invisible to users")
	assert.Contains(t, snapshot["admin_info"].(map[string]any)["usage_discount"], "shadow_rates")

	// Fixed is live while month is shadow: the public object must not carry
	// the shadow month rate in any field.
	mixed := usageDiscountTestSettings(t, 5, func(c *model.UsageDiscountConfig) {
		c.Site.Mode = model.UsageDiscountModeOff
		c.Personal.Day.Mode, c.Personal.Week.Mode, c.Personal.Month.Mode = model.UsageDiscountModeOff, model.UsageDiscountModeOff, model.UsageDiscountModeShadow
	})
	other = model.NewLogOther()
	appendUsageDiscountInfo(other, hosttypes.ChannelRatioInfo{ChannelRatio: 1.98, BaseChannelRatio: 2, UsageDiscount: evaluateUsageDiscount(mixed, &usageDiscountAmounts{Month: 1000})})
	snapshot = other.Snapshot()
	public = snapshot["usage_discount"].(map[string]any)
	assert.Equal(t, "0", public["month_rate"])
	assert.Equal(t, "0.99", public["factor"])
	assert.NotContains(t, public, "winner_window")
	encodedPublic, err := common.Marshal(public)
	require.NoError(t, err)
	assert.NotContains(t, string(encodedPublic), "0.05")
	assert.Equal(t, map[string]string{usageDiscountWindowMonth: "0.05"}, snapshot["admin_info"].(map[string]any)["usage_discount"].(map[string]any)["shadow_rates"])

	for _, ratio := range []hosttypes.ChannelRatioInfo{{ChannelRatio: 2}, {ChannelRatio: 2, BaseChannelRatio: 2, UsageDiscount: hosttypes.NeutralUsageDiscount(hosttypes.UsageDiscountInactive, "")}} {
		other = model.NewLogOther()
		appendUsageDiscountInfo(other, ratio)
		assert.Empty(t, other.Snapshot())
	}
}
