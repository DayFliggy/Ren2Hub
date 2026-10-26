//go:build !windows

package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/shirou/gopsutil/load"
)

// sampleDashboardLoad reads the kernel load average (/proc/loadavg on Linux).
// gopsutil v3.21 indexes the file's fields without a length check, so a
// truncated file panics; the reading is reported unavailable instead.
func sampleDashboardLoad() (one, five, fifteen float64, ok bool) {
	defer func() {
		if recovered := recover(); recovered != nil {
			common.SysError(fmt.Sprintf("load average sample failed: %v", recovered))
			one, five, fifteen, ok = 0, 0, 0, false
		}
	}()
	avg, err := load.Avg()
	if err != nil {
		common.SysError(fmt.Sprintf("load average sample failed: %v", err))
		return 0, 0, 0, false
	}
	return avg.Load1, avg.Load5, avg.Load15, true
}
