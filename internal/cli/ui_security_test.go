package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"

	"github.com/aoci-spec/aoci-code/internal/ui"
)

func TestDetachPreservesLiveLegacyPageUntilExplicitStop(t *testing.T) {
	root, registry := t.TempDir(), t.TempDir()
	legacy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/repos" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]string{{"root": root}})
	}))
	defer legacy.Close()
	reg := ui.Registration{URL: legacy.URL, Root: root, PID: 42}
	if err := ui.Register(registry, reg); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	command := &cobra.Command{}
	command.SetOut(&output)
	err := detachUI(command, registry, root, 0, "127.0.0.1", false, nil, false)
	var exit *ExitError
	if !errors.As(err, &exit) || exit.Code != ExitConfig {
		t.Fatalf("live legacy page was not refused: %v", err)
	}
	if output.Len() != 0 {
		t.Fatal("legacy page was reported as a usable panel")
	}
	got, ok := ui.Registered(registry, root)
	if !ok || got.URL != reg.URL || got.PID != reg.PID {
		t.Fatal("legacy registration was replaced instead of preserved for --stop")
	}
}
