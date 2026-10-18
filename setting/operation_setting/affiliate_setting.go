package operation_setting

import (
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

// AffiliateSetting is the sole source of rules applied at successful credit time.
type AffiliateSetting struct {
	Enabled              bool `json:"enabled"`
	RateBps              int  `json:"rate_bps"`
	FreezeHours          int  `json:"freeze_hours"`
	DurationDays         int  `json:"duration_days"`
	PerInviteeCapQuota   int  `json:"per_invitee_cap_quota"`
	AdminRechargeEnabled bool `json:"admin_recharge_enabled"`
}

var affiliateSetting = AffiliateSetting{RateBps: 2000}

func init()                                  { config.GlobalConfig.Register("affiliate_setting", &affiliateSetting) }
func GetAffiliateSetting() *AffiliateSetting { return &affiliateSetting }
func ValidateAffiliateOption(key, value string) error {
	if !strings.HasPrefix(key, "affiliate_setting.") {
		return nil
	}
	name := strings.TrimPrefix(key, "affiliate_setting.")
	if name == "enabled" || name == "admin_recharge_enabled" {
		if value != "true" && value != "false" {
			return errors.New("affiliate option requires true or false")
		}
		return nil
	}
	maximum := 0
	switch name {
	case "rate_bps":
		maximum = 10000
	case "freeze_hours":
		maximum = 720
	case "duration_days":
		maximum = 3650
	case "per_invitee_cap_quota":
		maximum = common.MaxWalletQuota
	default:
		return errors.New("unknown affiliate option")
	}
	v, err := strconv.ParseInt(value, 10, 64)
	if err != nil || v < 0 || v > int64(maximum) {
		return errors.New("invalid affiliate option value")
	}
	return nil
}

// SnapshotAffiliateSetting shares the native option-map lock with option writes.
func SnapshotAffiliateSetting() AffiliateSetting {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return affiliateSetting
}
