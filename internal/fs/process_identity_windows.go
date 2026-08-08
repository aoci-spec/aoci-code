//go:build windows

package fs

import (
	"errors"
	"fmt"
	"syscall"
)

const (
	stillActive           = 259
	errorAccessDenied     = syscall.Errno(5)
	errorInvalidParameter = syscall.Errno(87)
)

// processIdentity以Windows进程创建FILETIME绑定PID，能识别PID复用。
func processIdentity(pid int) (string, bool, bool) {
	handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, errorInvalidParameter) {
			return "", false, true
		}
		if errors.Is(err, errorAccessDenied) {
			return "", true, false
		}
		return "", false, false
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return "", false, false
	}
	if exitCode != stillActive {
		return "", false, true
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &created, &exited, &kernel, &user); err != nil {
		return "", true, false
	}
	return fmt.Sprintf("%08x%08x", created.HighDateTime, created.LowDateTime), true, true
}
