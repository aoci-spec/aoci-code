//go:build windows

// Windows 控制台抑制 —— 与 git_command.go 同包的平台侧实现
//
// 威胁: aoci 常以无控制台形态运行(如 MCP stdio 由宿主拉起)。此时 exec 拉起
// 控制台子进程(git.exe)时,Windows 会为子进程新分配一个可见控制台窗口——
// 刷新期间反复弹窗并抢占用户焦点。
// 边界: CREATE_NO_WINDOW 只抑制控制台分配,不改变 stdio 句柄继承;宿主提供的
// 管道与真实终端下的交互输出不受影响。HideWindow(STARTF_USESHOWWINDOW+SW_HIDE)
// 作为兜底。调用方环境变量属操作者自身信任域,不在此处覆盖。
package fs

import (
	"os/exec"
	"syscall"
)

// createNoWindow 即 Win32 CREATE_NO_WINDOW(0x08000000)。
const createNoWindow = 0x08000000

// hideChildConsole 阻止 Windows 为 git.exe 子进程新建可见控制台窗口。
func hideChildConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
