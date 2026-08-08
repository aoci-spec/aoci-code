//go:build (aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris) && !linux

package fs

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// 这些Unix没有统一的/proc启动tick；ps lstart仍能为同一PID提供稳定的
// 启动身份，使陈旧锁在PID复用后可证明不再属于原持有者。
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
