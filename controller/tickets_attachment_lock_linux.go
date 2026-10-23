//go:build linux

package controller

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const ticketAttachmentCleanupSupported = true

func lockTicketUpload(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}

func tryLockTicketCleanupFile(file *os.File) (bool, error) {
	err := lockTicketUpload(file)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return err == nil, err
}

func openTicketCleanupFile(root *os.Root, name string) (*os.File, error) {
	// Do not follow the final path component or block if a racing replacement
	// turns a regular file into a FIFO. The caller rechecks the open inode.
	return root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
}
