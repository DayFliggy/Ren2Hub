package model

import "strings"

func retiredRoutingOption(key string) bool {
	switch key {
	case "AutoGroups", "MaxTokenAutoGroups", "DefaultUseAutoGroup", "GroupGroupRatio", "UserUsableGroups", "GroupRatio", "TopupGroupRatio":
		return true
	}
	return strings.HasPrefix(key, "group_ratio_setting.") || strings.HasPrefix(key, "ModelRequestRateLimit")
}

func IsRetiredRoutingOption(key string) bool {
	return retiredRoutingOption(key)
}
