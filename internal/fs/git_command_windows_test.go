//go:build windows

package fs

import (
	"testing"
)

// When a parent without a console (an MCP stdio host, for example) starts git.exe, Windows allocates a new visible
// console window for the child. The built command must carry CREATE_NO_WINDOW+HideWindow; this assertion pins
// the window suppression as a constructor invariant so a later refactor cannot drop it silently.
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
