package model

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRetiredBusinessOptionsRejectEveryValue(t *testing.T) {
	for _, key := range []string{"GroupRatio", "GroupGroupRatio", "GroupSpecialUsableGroup", "UserUsableGroups", "AutoGroups", "DefaultUseAutoGroup", "MaxTokenAutoGroups", "TopupGroupRatio", "ModelRequestRateLimitGroup", "QuotaForInviter", "QuotaForInvitee", "group_ratio_setting.group_ratio"} {
		for _, value := range []string{"", "0", "-1", "999999", "{}", "[]"} {
			t.Run(key+"/"+value, func(t *testing.T) { require.Error(t, validateOptionValue(key, value)) })
		}
	}
}
