package service

import (
	"os"
	"syscall"
)

// RestartSystemProcess enters the signal-driven graceful shutdown path in main.
func RestartSystemProcess() error {
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}
	return process.Signal(syscall.SIGTERM)
}
