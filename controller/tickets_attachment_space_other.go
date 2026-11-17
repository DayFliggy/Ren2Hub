//go:build !linux

package controller

// 产品只交付 Linux 镜像；其他平台明确报告不支持，调用方跳过可用空间检查。
func ticketFilesystemFreeSpace(_ string) (uint64, error) {
	return 0, errTicketFreeSpaceUnsupported
}
