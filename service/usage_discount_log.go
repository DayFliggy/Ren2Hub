package service

import (
	"github.com/QuantumNous/new-api/model"
	hosttypes "github.com/QuantumNous/new-api/types"
)

// appendUsageDiscountInfo explains how channel_ratio (the effective ratio)
// was derived. Users see the object only when a component can change the
// price; administrators also see shadow-only decisions. Neither view carries
// the site total or the exclusion list.
func appendUsageDiscountInfo(other *model.LogOther, ratio hosttypes.ChannelRatioInfo) {
	decision := ratio.UsageDiscount
	if other == nil || decision == nil || decision.Status == hosttypes.UsageDiscountInactive {
		return
	}
	if decision.Active {
		public := map[string]any{
			"status":             decision.Status,
			"factor":             decision.Factor.String(),
			"applied_rate":       decision.AppliedRate.String(),
			"fixed_rate":         decision.FixedRate.String(),
			"site_rate":          decision.SiteRate.String(),
			"day_rate":           decision.DayRate.String(),
			"week_rate":          decision.WeekRate.String(),
			"month_rate":         decision.MonthRate.String(),
			"cap_applied":        decision.CapApplied,
			"base_channel_ratio": FormatChannelRatio(ratio.BaseRatio()),
		}
		if decision.WinnerWindow != "" {
			public["winner_window"] = decision.WinnerWindow
		}
		other.SetPublic("usage_discount", public)
	}
	admin := map[string]any{
		"status":         decision.Status,
		"raw_rate":       decision.RawRate.String(),
		"cap_rate":       decision.CapRate.String(),
		"config_version": decision.ConfigVersion,
	}
	if len(decision.ShadowRates) > 0 {
		admin["shadow_rates"] = decision.ShadowRates
	}
	if decision.BusinessDate != "" {
		admin["business_date"] = decision.BusinessDate
	}
	if decision.Reason != "" {
		admin["reason"] = decision.Reason
	}
	other.SetAdmin("usage_discount", admin)
}
