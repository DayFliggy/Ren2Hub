package common

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/shirou/gopsutil/cpu"
	"github.com/shirou/gopsutil/mem"
)

type DiskSpaceInfo struct {
	Total       uint64  `json:"total"`
	Free        uint64  `json:"free"`
	Used        uint64  `json:"used"`
	UsedPercent float64 `json:"used_percent"`
}

// SystemStatus is the latest sampled host state. Availability flags prevent a
// zero-valued failed sample from being presented as a real measurement.
type SystemStatus struct {
	CPUUsage         float64
	CPUAvailable     bool
	MemoryUsage      float64
	MemoryAvailable  bool
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64
	DiskUsage        float64
	DiskAvailable    bool
	DiskUsedBytes    uint64
	DiskTotalBytes   uint64
	Network          NetworkBandwidth
}

var latestSystemStatus atomic.Value
var latestNetworkBandwidth atomic.Value

func init() {
	latestSystemStatus.Store(SystemStatus{})
	latestNetworkBandwidth.Store(NetworkBandwidth{})
}

func StartSystemMonitor() {
	BackgroundWorkers.Go(startHostSystemMonitor)
	BackgroundWorkers.Go(startNetworkSystemMonitor)
}

func startHostSystemMonitor(ctx context.Context) {
	for ctx.Err() == nil {
		config := GetPerformanceMonitorConfig()
		if !config.Enabled {
			latestSystemStatus.Store(SystemStatus{})
			if !WaitBackgroundInterval(ctx, 30*time.Second) {
				return
			}
			continue
		}

		updateSystemStatus()
		if !WaitBackgroundInterval(ctx, 5*time.Second) {
			return
		}
	}
}

func startNetworkSystemMonitor(ctx context.Context) {
	for ctx.Err() == nil {
		config := GetPerformanceMonitorConfig()
		if !config.Enabled {
			ResetNetworkBandwidthSampler()
			latestNetworkBandwidth.Store(NetworkBandwidth{})
			if !WaitBackgroundInterval(ctx, 30*time.Second) {
				return
			}
			continue
		}

		latestNetworkBandwidth.Store(SampleNetworkBandwidth())
		if !WaitBackgroundInterval(ctx, 5*time.Second) {
			return
		}
	}
}

func updateSystemStatus() {
	var status SystemStatus

	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		status.CPUUsage = percents[0]
		status.CPUAvailable = true
	}

	if memInfo, err := mem.VirtualMemory(); err == nil {
		status.MemoryUsage = memInfo.UsedPercent
		status.MemoryUsedBytes = memInfo.Used
		status.MemoryTotalBytes = memInfo.Total
		status.MemoryAvailable = memInfo.Total > 0
	}

	diskInfo := GetDiskSpaceInfo()
	if diskInfo.Total > 0 {
		status.DiskUsage = diskInfo.UsedPercent
		status.DiskAvailable = true
		status.DiskUsedBytes = diskInfo.Used
		status.DiskTotalBytes = diskInfo.Total
	}

	latestSystemStatus.Store(status)
}

func GetSystemStatus() SystemStatus {
	if !GetPerformanceMonitorConfig().Enabled {
		return SystemStatus{}
	}
	status := latestSystemStatus.Load().(SystemStatus)
	status.Network = latestNetworkBandwidth.Load().(NetworkBandwidth)
	return status
}

func resetSystemMonitorState() {
	latestSystemStatus.Store(SystemStatus{})
	latestNetworkBandwidth.Store(NetworkBandwidth{})
	ResetNetworkBandwidthSampler()
}
