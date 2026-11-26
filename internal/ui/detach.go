package ui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
// stderr goes to logPath; stdout carries readiness or a CLI startup error.
func Detach(executable string, args []string, logPath string, wait time.Duration) (DetachResult, error) {
	cmd := exec.Command(executable, args...)
	cmd.SysProcAttr = detachAttr()
	cmd.Stdin = nil
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
			return DetachResult{}, fmt.Errorf("create panel log directory: %w", err)
		}
		log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return DetachResult{}, fmt.Errorf("open panel log: %w", err)
		}
		cmd.Stderr = log
		defer log.Close()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return DetachResult{}, err
	}
	defer stdout.Close()
	if err := cmd.Start(); err != nil {
		return DetachResult{}, err
	}
	pid := cmd.Process.Pid
	type startup struct {
		ready DetachResult
		err   error
	}
	result := make(chan startup, 1)
	go func() {
		ready, err := readDetachStartup(stdout)
		result <- startup{ready: ready, err: err}
	}()
	select {
	case got := <-result:
		if got.err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return DetachResult{}, got.err
		}
		_ = cmd.Process.Release()
		got.ready.PID = pid
		return got.ready, nil
	case <-time.After(wait):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return DetachResult{}, fmt.Errorf("page did not report readiness within %s", wait)
	}
}

const detachStartupMaxBytes = 64 << 10

func readDetachStartup(stdout io.Reader) (DetachResult, error) {
	reader := bufio.NewReader(io.LimitReader(stdout, detachStartupMaxBytes+1))
	line, err := reader.ReadBytes('\n')
	if len(bytes.TrimSpace(line)) == 0 && err != nil {
		return DetachResult{}, fmt.Errorf("page exited before reporting readiness")
	}
	if len(line) > detachStartupMaxBytes {
		return DetachResult{}, fmt.Errorf("page startup output exceeds %d bytes", detachStartupMaxBytes)
	}
	var ready struct {
		URL          string `json:"url"`
		Repositories int    `json:"repositories"`
	}
	if json.Unmarshal(line, &ready) == nil && ready.URL != "" {
		return DetachResult{URL: ready.URL, Repositories: ready.Repositories}, nil
	}

	// The same reader owns buffered bytes after the first line. The CLI's
	// startup error is an indented JSON object, and the child then exits.
	rest, err := io.ReadAll(reader)
	if len(line)+len(rest) > detachStartupMaxBytes {
		return DetachResult{}, fmt.Errorf("page startup output exceeds %d bytes", detachStartupMaxBytes)
	}
	var failure struct {
		OK       *bool  `json:"ok"`
		ExitCode int    `json:"exit_code"`
		Message  string `json:"message"`
	}
	if err == nil && json.Unmarshal(append(line, rest...), &failure) == nil &&
		failure.OK != nil && !*failure.OK && failure.ExitCode != 0 && failure.Message != "" {
		return DetachResult{}, fmt.Errorf("%s", failure.Message)
	}
	return DetachResult{}, fmt.Errorf("unexpected panel startup output")
}
