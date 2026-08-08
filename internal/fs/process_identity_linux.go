//go:build linux

package fs

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
)

// processIdentity使用/proc stat的进程启动tick绑定PID，能识别PID复用。
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
	// 右括号后的首字段是stat字段3；进程启动tick为字段22，即下标19。
	if len(fields) <= 19 {
		return "", true, false
	}
	// stat字段3是进程状态；zombie/dead进程仍可能让kill(pid,0)成功，
	// 但已经不可能继续持有或恢复AOCI临界区，必须允许回收其陈旧锁。
	if fields[0] == "Z" || fields[0] == "X" || fields[0] == "x" {
		return fields[19], false, true
	}
	return fields[19], true, true
}
