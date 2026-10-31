//go:build linux

package fs

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// processIdentity binds the PID to the process start tick from /proc stat, which detects PID reuse.
func processIdentity(pid int) (string, bool, bool) {
	err := syscall.Kill(pid, 0)
	switch {
	case errors.Is(err, syscall.ESRCH):
		return "", false, true
	case err != nil && !errors.Is(err, syscall.EPERM):
		return "", false, false
	}
	data, readErr := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if os.IsNotExist(readErr) {
		return "", false, true
	}
	if readErr != nil {
		return "", true, false
	}
	closing := strings.LastIndexByte(string(data), ')')
	if closing < 0 {
		return "", true, false
	}
	fields := strings.Fields(string(data[closing+1:]))
	// The first field after the closing parenthesis is stat field 3; the start tick is field 22, index 19.
	if len(fields) <= 19 {
		return "", true, false
	}
	// stat field 3 is the process state; a zombie or dead process may still let kill(pid,0) succeed,
	// but it can no longer hold or recover an AOCI critical section, so its stale lock must be reclaimable.
	if fields[0] == "Z" || fields[0] == "X" || fields[0] == "x" {
		return fields[19], false, true
	}
	return fields[19], true, true
}
