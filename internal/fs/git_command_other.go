//go:build !windows

package fs

import "os/exec"

// hideChildConsole is a no-op off Windows; the visible console window problem exists only there.
func hideChildConsole(*exec.Cmd) {}
