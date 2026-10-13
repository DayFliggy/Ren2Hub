package operation_setting

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

const LogMaxRangeDaysOptionKey = "log_setting.max_range_days"
const LogUserExportOptionKey = "log_setting.user_export_enabled"

type LogSetting struct {
	UserExportEnabled bool `json:"user_export_enabled"`
	MaxRangeDays      int  `json:"max_range_days"`
}

var logSetting = LogSetting{UserExportEnabled: true, MaxRangeDays: 365}

func init()                      { config.GlobalConfig.Register("log_setting", &logSetting) }
func GetLogSetting() *LogSetting { return &logSetting }
func (s *LogSetting) MaxRangeSeconds() int64 {
	return int64(max(1, min(s.MaxRangeDays, 36500))) * 86400
}
func ValidateLogMaxRangeDays(value string) error {
	days, err := strconv.Atoi(value)
	if err != nil || days < 1 || days > 36500 {
		return common.NewMessage("The log time range must be between 1 and 36500 days")
	}
	return nil
}
