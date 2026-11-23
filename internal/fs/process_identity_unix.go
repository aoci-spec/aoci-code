//go:build (aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris) && !linux

package fs

import (
	"errors"
	"os"
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
	// ps prints lstart in the caller's locale and time zone, so two processes
	// with different TZ or LC_TIME saw different identities for one live PID
	// and a waiter took a live holder's lock (audit G1-05). Fix both, and mark
	// the identity so one recorded before the fix is never compared with it.
	command := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid))
	command.Env = append(os.Environ(), "LC_ALL=C", "LANG=C", "TZ=UTC")
	output, lookupErr := command.Output()
	identity := strings.TrimSpace(string(output))
	if lookupErr != nil || identity == "" {
		return "", true, false
	}
	return lstartIdentityPrefix + identity, true, true
}

const lstartIdentityPrefix = "lstart-c-utc:"

// identitiesComparable is false for an identity recorded before the format
// was fixed: it may differ from a fresh one only by locale or time zone.
func identitiesComparable(recorded, current string) bool {
	return strings.HasPrefix(recorded, lstartIdentityPrefix) && strings.HasPrefix(current, lstartIdentityPrefix)
}
