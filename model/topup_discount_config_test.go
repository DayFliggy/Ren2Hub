package model

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodeTopUpDiscountConfig(t *testing.T, mutate func(*TopUpDiscountConfig)) []byte {
	t.Helper()
	config := DefaultTopUpDiscountConfig()
	if mutate != nil {
		mutate(&config)
	}
	data, err := common.Marshal(config)
	require.NoError(t, err)
	return data
}

func TestDefaultTopUpDiscountConfigIsValidAndChangesNoPrice(t *testing.T) {
	settings, err := ParseTopUpDiscountConfig(encodeTopUpDiscountConfig(t, nil))
	require.NoError(t, err)
	assert.True(t, settings.Enabled(), "personal shadow tables still collect data")
	assert.False(t, settings.Active(), "the default must not change any price")
	assert.True(t, settings.IsExcluded(1))
	assert.Equal(t, int64(12_500_000), settings.Month.Tiers[0].Threshold)
	assert.Equal(t, "0.05", settings.Month.Tiers[4].Rate.String())
	assert.Equal(t, int64(1_250_000_000), settings.Month.Tiers[4].Threshold)
}

func TestTopUpDiscountConfigRejectsInvalidDocuments(t *testing.T) {
	cases := map[string]func(*TopUpDiscountConfig){
		"five fraction digits": func(c *TopUpDiscountConfig) { c.FixedRate = "0.00001" },
		"negative rate":        func(c *TopUpDiscountConfig) { c.FixedRate = "-0.01" },
		"exponent":             func(c *TopUpDiscountConfig) { c.CapRate = "5e-2" },
		"leading zero":         func(c *TopUpDiscountConfig) { c.CapRate = "00.05" },
		"empty rate":           func(c *TopUpDiscountConfig) { c.FixedRate = "" },
		"cap above half":       func(c *TopUpDiscountConfig) { c.CapRate = "0.6" },
		"fixed above cap":      func(c *TopUpDiscountConfig) { c.FixedRate = "0.06" },
		"tier above cap":       func(c *TopUpDiscountConfig) { c.Personal.Day.Tiers[4].Rate = "0.06" },
		"unknown mode":         func(c *TopUpDiscountConfig) { c.Site.Mode = "auto" },
		"equal thresholds": func(c *TopUpDiscountConfig) {
			c.Personal.Week.Tiers[1].ThresholdQuota = c.Personal.Week.Tiers[0].ThresholdQuota
		},
		"decreasing rate":         func(c *TopUpDiscountConfig) { c.Personal.Month.Tiers[1].Rate = "0.005" },
		"threshold with sign":     func(c *TopUpDiscountConfig) { c.Personal.Day.Tiers[0].ThresholdQuota = "+1" },
		"threshold decimal":       func(c *TopUpDiscountConfig) { c.Personal.Day.Tiers[0].ThresholdQuota = "1.5" },
		"threshold 19 digits":     func(c *TopUpDiscountConfig) { c.Personal.Day.Tiers[4].ThresholdQuota = "1000000000000000000" },
		"eleven tiers":            func(c *TopUpDiscountConfig) { c.Site.Tiers = make([]TopUpDiscountTierConfig, 11) },
		"empty timezone":          func(c *TopUpDiscountConfig) { c.Timezone = "" },
		"local timezone":          func(c *TopUpDiscountConfig) { c.Timezone = "Local" },
		"unknown timezone":        func(c *TopUpDiscountConfig) { c.Timezone = "Mars/Base" },
		"duplicate excluded user": func(c *TopUpDiscountConfig) { c.ExcludedUserIDs = []int{3, 3} },
		"zero excluded user":      func(c *TopUpDiscountConfig) { c.ExcludedUserIDs = []int{0} },
		"too many excluded users": func(c *TopUpDiscountConfig) { c.ExcludedUserIDs = make([]int, 101) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTopUpDiscountConfig(encodeTopUpDiscountConfig(t, mutate))
			assert.ErrorIs(t, err, ErrTopUpDiscountInvalidConfig)
		})
	}

	_, err := ParseTopUpDiscountConfig(encodeTopUpDiscountConfig(t, func(c *TopUpDiscountConfig) { c.SchemaVersion = 2 }))
	assert.ErrorIs(t, err, ErrTopUpDiscountSchemaUnsupported)

	for name, raw := range map[string]string{
		"number rate":     `{"schema_version":1,"timezone":"UTC","cap_rate":0.05,"fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}`,
		"unknown field":   `{"schema_version":1,"version":7,"timezone":"UTC","cap_rate":"0.05","fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}`,
		"trailing object": `{"schema_version":1,"timezone":"UTC","cap_rate":"0.05","fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTopUpDiscountConfig([]byte(raw))
			assert.ErrorIs(t, err, ErrTopUpDiscountInvalidConfig)
		})
	}
}

func TestTopUpDiscountConfigNormalizesDecimals(t *testing.T) {
	settings, err := ParseTopUpDiscountConfig(encodeTopUpDiscountConfig(t, func(c *TopUpDiscountConfig) {
		c.CapRate, c.FixedRate = "0.0500", "0.010"
		c.Personal.Day.Tiers[0].Rate = "0.0100"
		c.ExcludedUserIDs = nil
	}))
	require.NoError(t, err)
	assert.Equal(t, "0.05", settings.Config.CapRate)
	assert.Equal(t, "0.01", settings.Config.FixedRate)
	assert.Equal(t, "0.01", settings.Config.Personal.Day.Tiers[0].Rate)
	assert.Equal(t, []int{}, settings.Config.ExcludedUserIDs)
	assert.Equal(t, []TopUpDiscountTierConfig{}, settings.Config.Site.Tiers)
}

func TestTopUpDiscountZoneMustKeepWholeHourMidnights(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, zone := range []string{"Asia/Shanghai", "UTC", "America/New_York", "Europe/London"} {
		location, err := time.LoadLocation(zone)
		require.NoError(t, err)
		assert.NoError(t, validateTopUpDiscountZoneAhead(location, now), zone)
	}
	for _, zone := range []string{"Asia/Kolkata", "Asia/Kathmandu", "Australia/Lord_Howe"} {
		location, err := time.LoadLocation(zone)
		require.NoError(t, err)
		assert.ErrorIs(t, validateTopUpDiscountZoneAhead(location, now), ErrTopUpDiscountInvalidConfig, zone)
	}
}
