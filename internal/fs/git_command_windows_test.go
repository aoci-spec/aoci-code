//go:build windows

package fs

import (
	"testing"
)

// 无控制台父进程(如 MCP stdio 宿主)拉起 git.exe 时,Windows 会为子进程新分配
// 可见控制台窗口。构造出的命令必须携带 CREATE_NO_WINDOW+HideWindow;本断言把
// 窗口抑制固定为构造器不变量,防止后续重构静默丢失。
func TestUntrustedRepositoryGitCommandSuppressesChildConsoleWindow(t *testing.T) {
	command := UntrustedRepositoryGitCommand(t.TempDir(), "rev-parse", "--show-toplevel")
	attr := command.SysProcAttr
	if attr == nil {
		t.Fatalf("git 调用必须设置 SysProcAttr 以抑制子进程控制台窗口")
	}
	if !attr.HideWindow {
		t.Fatalf("git 调用必须设置 HideWindow")
	}
	if attr.CreationFlags&createNoWindow == 0 {
		t.Fatalf("git 调用必须携带 CREATE_NO_WINDOW(0x%x),实际 CreationFlags=0x%x",
			createNoWindow, attr.CreationFlags)
	}
}
