//go:build linux

package controller

import (
	"errors"
	"math"

	"golang.org/x/sys/unix"
)

// ticketFilesystemFreeSpace 返回非特权进程在 dir 所在文件系统上可用的字节（Bavail）。
func ticketFilesystemFreeSpace(dir string) (uint64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	if stat.Bsize <= 0 {
		return 0, errors.New("statfs reported a non-positive block size")
	}
	blockSize := uint64(stat.Bsize)
	if stat.Bavail > math.MaxUint64/blockSize {
		return math.MaxUint64, nil
	}
	return stat.Bavail * blockSize, nil
}
