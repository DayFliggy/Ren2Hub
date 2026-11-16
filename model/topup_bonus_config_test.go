package model

import (
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodeTopUpBonusConfig(t *testing.T, mutate func(*TopUpBonusConfig)) []byte {
	t.Helper()
	config := DefaultTopUpBonusConfig()
	if mutate != nil {
		mutate(&config)
	}
	data, err := common.Marshal(config)
	require.NoError(t, err)
	return data
}

func TestDefaultTopUpBonusConfigIsValidAndGrantsNoBonus(t *testing.T) {
	settings, err := ParseTopUpBonusConfig(encodeTopUpBonusConfig(t, nil))
	require.NoError(t, err)
	assert.True(t, settings.Enabled(), "personal shadow tables still collect data")
	assert.False(t, settings.Active(), "the default must not grant any bonus")
	assert.True(t, settings.IsExcluded(1))
	assert.Equal(t, int64(12_500_000), settings.Month.Tiers[0].Threshold)
	assert.Equal(t, "0.05", settings.Month.Tiers[4].Rate.String())
	assert.Equal(t, int64(1_250_000_000), settings.Month.Tiers[4].Threshold)
}

func TestTopUpBonusConfigRejectsInvalidDocuments(t *testing.T) {
	cases := map[string]func(*TopUpBonusConfig){
		"five fraction digits": func(c *TopUpBonusConfig) { c.FixedRate = "0.00001" },
		"negative rate":        func(c *TopUpBonusConfig) { c.FixedRate = "-0.01" },
		"exponent":             func(c *TopUpBonusConfig) { c.CapRate = "5e-2" },
		"leading zero":         func(c *TopUpBonusConfig) { c.CapRate = "00.05" },
		"empty rate":           func(c *TopUpBonusConfig) { c.FixedRate = "" },
		"cap above half":       func(c *TopUpBonusConfig) { c.CapRate = "0.6" },
		"fixed above cap":      func(c *TopUpBonusConfig) { c.FixedRate = "0.06" },
		"tier above cap":       func(c *TopUpBonusConfig) { c.Personal.Day.Tiers[4].Rate = "0.06" },
		"unknown mode":         func(c *TopUpBonusConfig) { c.Site.Mode = "auto" },
		"equal thresholds": func(c *TopUpBonusConfig) {
			c.Personal.Week.Tiers[1].ThresholdQuota = c.Personal.Week.Tiers[0].ThresholdQuota
		},
		"decreasing rate":         func(c *TopUpBonusConfig) { c.Personal.Month.Tiers[1].Rate = "0.005" },
		"threshold with sign":     func(c *TopUpBonusConfig) { c.Personal.Day.Tiers[0].ThresholdQuota = "+1" },
		"threshold decimal":       func(c *TopUpBonusConfig) { c.Personal.Day.Tiers[0].ThresholdQuota = "1.5" },
		"threshold 19 digits":     func(c *TopUpBonusConfig) { c.Personal.Day.Tiers[4].ThresholdQuota = "1000000000000000000" },
		"eleven tiers":            func(c *TopUpBonusConfig) { c.Site.Tiers = make([]TopUpBonusTierConfig, 11) },
		"empty timezone":          func(c *TopUpBonusConfig) { c.Timezone = "" },
		"local timezone":          func(c *TopUpBonusConfig) { c.Timezone = "Local" },
		"unknown timezone":        func(c *TopUpBonusConfig) { c.Timezone = "Mars/Base" },
		"duplicate excluded user": func(c *TopUpBonusConfig) { c.ExcludedUserIDs = []int{3, 3} },
		"zero excluded user":      func(c *TopUpBonusConfig) { c.ExcludedUserIDs = []int{0} },
		"too many excluded users": func(c *TopUpBonusConfig) { c.ExcludedUserIDs = make([]int, 101) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTopUpBonusConfig(encodeTopUpBonusConfig(t, mutate))
			assert.ErrorIs(t, err, ErrTopUpBonusInvalidConfig)
		})
	}

	_, err := ParseTopUpBonusConfig(encodeTopUpBonusConfig(t, func(c *TopUpBonusConfig) { c.SchemaVersion = 2 }))
	assert.ErrorIs(t, err, ErrTopUpBonusSchemaUnsupported)

	for name, raw := range map[string]string{
		"number rate":     `{"schema_version":1,"timezone":"UTC","cap_rate":0.05,"fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}`,
		"unknown field":   `{"schema_version":1,"version":7,"timezone":"UTC","cap_rate":"0.05","fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}`,
		"trailing object": `{"schema_version":1,"timezone":"UTC","cap_rate":"0.05","fixed_rate":"0","site":{"mode":"off","tiers":[]},"personal":{"day":{"mode":"off","tiers":[]},"week":{"mode":"off","tiers":[]},"month":{"mode":"off","tiers":[]}},"excluded_user_ids":[]}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseTopUpBonusConfig([]byte(raw))
			assert.ErrorIs(t, err, ErrTopUpBonusInvalidConfig)
		})
	}
}

func TestTopUpBonusConfigNormalizesDecimals(t *testing.T) {
	settings, err := ParseTopUpBonusConfig(encodeTopUpBonusConfig(t, func(c *TopUpBonusConfig) {
		c.CapRate, c.FixedRate = "0.0500", "0.010"
		c.Personal.Day.Tiers[0].Rate = "0.0100"
		c.ExcludedUserIDs = nil
	}))
	require.NoError(t, err)
	assert.Equal(t, "0.05", settings.Config.CapRate)
	assert.Equal(t, "0.01", settings.Config.FixedRate)
	assert.Equal(t, "0.01", settings.Config.Personal.Day.Tiers[0].Rate)
	assert.Equal(t, []int{}, settings.Config.ExcludedUserIDs)
	assert.Equal(t, []TopUpBonusTierConfig{}, settings.Config.Site.Tiers)
}

func TestTopUpBonusZoneMustKeepWholeHourMidnights(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, zone := range []string{"Asia/Shanghai", "UTC", "America/New_York", "Europe/London"} {
		location, err := time.LoadLocation(zone)
		require.NoError(t, err)
		assert.NoError(t, validateTopUpBonusZoneAhead(location, now), zone)
	}
	for _, zone := range []string{"Asia/Kolkata", "Asia/Kathmandu", "Australia/Lord_Howe"} {
		location, err := time.LoadLocation(zone)
		require.NoError(t, err)
		assert.ErrorIs(t, validateTopUpBonusZoneAhead(location, now), ErrTopUpBonusInvalidConfig, zone)
	}
}
