package operation_setting

import "testing"

func TestAffiliateOptionValidation(t *testing.T) {
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"affiliate_setting.enabled", "true", true}, {"affiliate_setting.enabled", "1", false},
		{"affiliate_setting.rate_bps", "2000", true}, {"affiliate_setting.rate_bps", "10001", false},
		{"affiliate_setting.freeze_hours", "720", true}, {"affiliate_setting.freeze_hours", "721", false},
		{"affiliate_setting.freeze_hours", "0", true}, {"affiliate_setting.freeze_hours", "-1", false},
		{"affiliate_setting.duration_days", "3650", true}, {"affiliate_setting.duration_days", "3651", false},
		{"affiliate_setting.duration_days", "0", true}, {"affiliate_setting.duration_days", "1.1", false},
		{"affiliate_setting.per_invitee_cap_quota", "9007199254740991", true}, {"affiliate_setting.per_invitee_cap_quota", "9007199254740992", false},
		{"affiliate_setting.admin_recharge_enabled", "false", true}, {"affiliate_setting.unknown", "1", false},
		{"unrelated.setting", "anything", true},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			err := ValidateAffiliateOption(tc.key, tc.value)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v err=%v", tc.valid, err)
			}
		})
	}
}
