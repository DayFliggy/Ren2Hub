package operation_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateLogRangeRejectsOverflowAndInvalidDays(t *testing.T) {
	for _, value := range []string{"0", "-1", "36501", "9223372036854775807", "1.5", "bad"} {
		assert.Error(t, ValidateLogMaxRangeDays(value), value)
	}
	for _, value := range []string{"1", "365", "36500"} {
		assert.NoError(t, ValidateLogMaxRangeDays(value), value)
	}
}
