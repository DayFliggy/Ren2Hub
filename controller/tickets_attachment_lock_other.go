//go:build !linux

package controller

import "os"

const ticketAttachmentCleanupSupported = false

func lockTicketUpload(_ *os.File) error { return nil }

func tryLockTicketCleanupFile(_ *os.File) (bool, error) {
	return false, errTicketCleanupUnsupported
}

func openTicketCleanupFile(_ *os.Root, _ string) (*os.File, error) {
	return nil, errTicketCleanupUnsupported
}
