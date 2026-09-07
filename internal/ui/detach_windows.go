//go:build windows

package ui

import (
	"os"
	"syscall"
)

const detachedProcess = 0x00000008

// detachAttr starts the page without a console and in its own process
// group, so closing the starting console does not end it.
func detachAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess, HideWindow: true}
}

// terminate ends the page; Windows has no graceful signal for a process
// without a console, so the caller removes the registration itself.
func terminate(process *os.Process) error {
	return process.Kill()
}
