//go:build !windows

package ui

import (
	"os"
	"syscall"
)

// detachAttr puts the page in its own session so the shell that started it
// can exit, and its job control cannot reach the page.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// terminate asks the page to shut down gracefully; the page's own signal
// handling unregisters it before exiting.
func terminate(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}
