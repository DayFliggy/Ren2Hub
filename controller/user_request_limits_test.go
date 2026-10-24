package controller

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

func TestSelfUserRequestLimitsFollowEffectiveConfiguration(t *testing.T) {
	previous := setting.CurrentModelRequestLimits()
	t.Cleanup(func() {
		setting.ModelRequestRateLimitEnabled = previous.Enabled
		setting.ModelRequestRateLimitDurationMinutes = previous.WindowMinutes
		setting.ModelRequestRateLimitCount = previous.TotalRequests
		setting.ModelRequestRateLimitSuccessCount = previous.SuccessfulRequests
	})
	user := &model.User{Id: 1, Role: common.RoleCommonUser}
	for _, enabled := range []bool{true, false} {
		setting.ModelRequestRateLimitEnabled = enabled
		setting.ModelRequestRateLimitDurationMinutes = 5
		setting.ModelRequestRateLimitCount = 0 // Unlimited total, separately from successful requests.
		setting.ModelRequestRateLimitSuccessCount = 80
		payload, err := json.Marshal(buildSelfUserData(user))
		if err != nil {
			t.Fatal(err)
		}
		var response struct {
			Limits setting.ModelRequestLimits `json:"request_limits"`
		}
		if err := json.Unmarshal(payload, &response); err != nil {
			t.Fatal(err)
		}
		if response.Limits != setting.CurrentModelRequestLimits() {
			t.Fatalf("self response lost the current limiter configuration: %+v", response.Limits)
		}
	}
}
