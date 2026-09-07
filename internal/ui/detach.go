package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DetachResult is what a detached start reports back.
type DetachResult struct {
	PID          int
	URL          string
	Repositories int
}

// Detach starts the page as its own session, detached from the calling
// shell, and waits for its readiness line. The child is the same binary
// running the same command in the foreground; only the process relationship
// differs, so a detached page behaves exactly like an interactive one. Its
// stderr goes to logPath; its stdout is read once, for the readiness line.
func Detach(executable string, args []string, logPath string, wait time.Duration) (DetachResult, error) {
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = detachAttr()
	cmd.Stdin = nil
	if logPath != "" {
		if log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			cmd.Stderr = log
			defer log.Close()
		}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return DetachResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return DetachResult{}, err
	}
	pid := cmd.Process.Pid
	type first struct {
		text string
		err  error
	}
	line := make(chan first, 1)
	go func() {
		text, err := bufio.NewReader(stdout).ReadString('\n')
		line <- first{text: text, err: err}
	}()
	select {
	case got := <-line:
		var ready struct {
			URL          string `json:"url"`
			Repositories int    `json:"repositories"`
		}
		text := strings.TrimSpace(got.text)
		if text == "" || json.Unmarshal([]byte(text), &ready) != nil || ready.URL == "" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			if got.err != nil && text == "" {
				return DetachResult{}, fmt.Errorf("page exited before reporting readiness")
			}
			return DetachResult{}, fmt.Errorf("unexpected readiness line: %q", text)
		}
		_ = cmd.Process.Release()
		return DetachResult{PID: pid, URL: ready.URL, Repositories: ready.Repositories}, nil
	case <-time.After(wait):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return DetachResult{}, fmt.Errorf("page did not report readiness within %s", wait)
	}
}
