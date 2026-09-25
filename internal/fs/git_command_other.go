//go:build !windows

package fs

import "os/exec"

// hideChildConsole 在非 Windows 平台为空操作;可见控制台窗口问题仅 Windows 存在。
func hideChildConsole(*exec.Cmd) {}
