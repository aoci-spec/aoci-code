package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/ui"
)

func TestUIStopFindsRelativeAlsoRegistration(t *testing.T) {
	parent, registry := t.TempDir(), t.TempDir()
	t.Chdir(parent)
	root, err := filepath.Abs("repo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOCI_UI_REGISTRY_DIR", registry)
	stale := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer stale.Close()
	for _, path := range []string{"repo", filepath.FromSlash("./repo/../repo"), root} {
		t.Run(path, func(t *testing.T) {
			reg := ui.Registration{Root: root, URL: stale.URL, PID: os.Getpid()}
			if err := ui.Register(registry, reg); err != nil {
				t.Fatal(err)
			}
			out, errs, code := runCLIBare(t, "ui", "--also", path, "--stop", "--json")
			if code != ExitOK {
				t.Fatalf("stop: exit=%d out=%s err=%s", code, out, errs)
			}
			var result struct {
				Stopped bool `json:"stopped"`
				PID     int  `json:"pid"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if result.Stopped || result.PID != reg.PID {
				t.Errorf("stale registration was not found: %+v", result)
			}
			if _, ok := ui.Registered(registry, root); ok {
				t.Error("stale registration survived --stop")
			}
		})
	}
}
