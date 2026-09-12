//go:build linux

package ui

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDiscoverRunningServersResolvesRootFromServerDirectory(t *testing.T) {
	serverDir := t.TempDir()
	absoluteRoot := t.TempDir()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"relative flag", []string{"--repo", "repository"}, filepath.Join(serverDir, "repository")},
		{"relative equals", []string{"--repo=../repository"}, filepath.Join(serverDir, "..", "repository")},
		{"current directory", []string{"--repo", "."}, serverDir},
		{"absolute flag", []string{"--repo", absoluteRoot}, absoluteRoot},
		{"no flag", nil, serverDir},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The child stays in a different working directory from this scanner.
			// Only argv[0] is changed: /proc still observes a real child process.
			cmd := exec.Command(os.Args[0], "-test.run=^TestDiscoveryServerProcess$", "--", "mcp")
			cmd.Args[0] = "aoci"
			cmd.Args = append(cmd.Args, tc.args...)
			cmd.Dir = serverDir
			cmd.Env = append(os.Environ(), "AOCI_DISCOVERY_TEST_PROCESS=1")
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				_ = stdin.Close()
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
			for _, instance := range discoverRunningServers() {
				if instance.PID != cmd.Process.Pid {
					continue
				}
				if instance.Root != tc.want {
					t.Fatalf("root=%q, want server-relative root %q", instance.Root, tc.want)
				}
				return
			}
			t.Fatal("running server was not discovered")
		})
	}
}

func TestDiscoveryServerProcess(t *testing.T) {
	if os.Getenv("AOCI_DISCOVERY_TEST_PROCESS") != "1" {
		return
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}
