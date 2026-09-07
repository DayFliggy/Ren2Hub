package model

import "strings"

func retiredRoutingOption(key string) bool {
	switch key {
	case "AutoGroups", "MaxTokenAutoGroups", "DefaultUseAutoGroup", "GroupGroupRatio", "UserUsableGroups":
		return true
	}
	return strings.HasPrefix(key, "group_ratio_setting.")
}
