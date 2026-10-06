//go:build !windows

package common

import (
	"github.com/shirou/gopsutil/load"
	"golang.org/x/sys/unix"
)

// sampleLoadAverage reads the kernel load average (/proc/loadavg on Linux).
func sampleLoadAverage() (LoadAverage, bool) {
	avg, err := load.Avg()
	if err != nil {
		return LoadAverage{}, false
	}
	return LoadAverage{One: avg.Load1, Five: avg.Load5, Fifteen: avg.Load15}, true
}

// GetDiskSpaceInfo 获取缓存目录所在磁盘的空间信息 (Unix/Linux/macOS)
func GetDiskSpaceInfo() DiskSpaceInfo {
	info := DiskSpaceInfo{}

	var stat unix.Statfs_t
	err := unix.Statfs(DiskSpacePath(), &stat)
	if err != nil {
		return info
	}

	// 计算磁盘空间 (显式转换以兼容 FreeBSD，其字段类型为 int64)
	bsize := uint64(stat.Bsize)
	info.Total = uint64(stat.Blocks) * bsize
	info.Free = uint64(stat.Bavail) * bsize
	info.Used = info.Total - uint64(stat.Bfree)*bsize

	if info.Total > 0 {
		info.UsedPercent = float64(info.Used) / float64(info.Total) * 100
	}

	return info
}
