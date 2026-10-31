//go:build windows

// Windows console suppression: the platform half of git_command.go in the same package
//
// Threat: aoci often runs without a console (an MCP stdio server started by the host). When exec starts
// a console child process (git.exe) there, Windows allocates a new visible console window for the child,
// flashing windows and stealing the user's focus throughout a refresh.
// Boundary: CREATE_NO_WINDOW only suppresses console allocation and does not change stdio handle inheritance;
// pipes supplied by the host and interactive output on a real terminal are unaffected. HideWindow
// (STARTF_USESHOWWINDOW+SW_HIDE) is the fallback. The caller's environment variables belong to the operator's trust domain and are not overridden.
package fs

import (
	"os/exec"
	"syscall"
)

// createNoWindow is the Win32 CREATE_NO_WINDOW (0x08000000).
const createNoWindow = 0x08000000

// hideChildConsole stops Windows from creating a visible console window for the git.exe child.
func hideChildConsole(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
