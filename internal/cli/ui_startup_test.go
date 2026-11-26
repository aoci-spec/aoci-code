package cli

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/ui"
)

func TestUIDetachReportsCLIStartupFailure(t *testing.T) {
	pinDefaultLocale(t)
	root := scopeActivationFixture(t)
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	_, port, err := net.SplitHostPort(occupied.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, host, port, want string }{
		{"non_loopback", "0.0.0.0", "0", "0.0.0.0"},
		{"occupied_port", "127.0.0.1", port, occupied.Addr().String()},
	} {
		t.Run(test.name, func(t *testing.T) {
			registry := filepath.Join(t.TempDir(), "registry")
			logPath := ui.LogPath(registry, root)
			t.Setenv("AOCI_UI_REGISTRY_DIR", registry)
			_, err := ui.Detach(os.Args[0], []string{
				"-test.run=^TestUIDetachCLIChild$", "--",
				"--repo", root, "--json", "ui", "--discover=false", "--host", test.host, "--port", test.port,
			}, logPath, 10*time.Second)
			if err == nil || !strings.Contains(err.Error(), test.want) || strings.Contains(err.Error(), "unexpected readiness") {
				t.Errorf("expected the foreground CLI's error for %s, got %v", test.want, err)
			}
			info, statErr := os.Stat(registry)
			if statErr != nil || !info.IsDir() || (runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0) {
				t.Fatalf("no private log directory after failed start: %v %v", info, statErr)
			}
			if _, err := os.Stat(logPath); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUIDetachCLIChild(t *testing.T) {
	if separator := slices.Index(os.Args, "--"); separator >= 0 {
		os.Exit(executeCLI(os.Args[separator+1:], os.Stdout, os.Stderr))
	}
}
