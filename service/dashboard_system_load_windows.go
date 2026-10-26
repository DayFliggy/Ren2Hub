//go:build windows

package service

// sampleDashboardLoad reports no load average: Windows has no kernel run-queue
// average, and gopsutil's emulation starts a goroutine that cannot be stopped.
func sampleDashboardLoad() (one, five, fifteen float64, ok bool) {
	return 0, 0, 0, false
}
