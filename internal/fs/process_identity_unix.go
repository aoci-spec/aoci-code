//go:build (aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris) && !linux

package fs

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// These Unixes have no uniform /proc start tick; ps lstart still gives the same PID a stable start
// identity, so a stale lock is provably no longer its original holder's after PID reuse.
func processIdentity(pid int) (string, bool, bool) {
	err := syscall.Kill(pid, 0)
	switch {
	case errors.Is(err, syscall.ESRCH):
		return "", false, true
	case err != nil && !errors.Is(err, syscall.EPERM):
		return "", false, false
	}
	output, lookupErr := exec.Command(
		"ps", "-o", "lstart=", "-p", strconv.Itoa(pid),
	).Output()
	identity := strings.TrimSpace(string(output))
	if lookupErr != nil || identity == "" {
		return "", true, false
	}
	return identity, true, true
}
