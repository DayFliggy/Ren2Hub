package setting

import "strings"

// These removed contracts must not survive as writable, silently ignored options.
func IsRetiredBusinessOption(key string) bool {
	if strings.HasPrefix(key, "group_ratio_setting.") {
		return true
	}
	switch key {
	case "GroupRatio", "GroupGroupRatio", "GroupSpecialUsableGroup", "UserUsableGroups", "AutoGroups", "DefaultUseAutoGroup", "MaxTokenAutoGroups", "TopupGroupRatio", "ModelRequestRateLimitGroup", "QuotaForInviter", "QuotaForInvitee":
		return true
	default:
		return false
	}
}
