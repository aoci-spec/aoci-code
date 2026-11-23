package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

// The MCP server and the UI viewer never write team configuration; a
// pre-Locale config is served as the legacy Locale and left for an ordinary
// CLI command to record (aoci-mcp-runtime-v1).
func TestInvocationLocaleObserveOnlyCommandsLeaveLegacyConfig(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	original := []byte("{\"version\":1,\"index_path\":\"aoci.txt\"}\n")
	setup := func(t *testing.T) string {
		root := t.TempDir()
		path := config.FilePath(root)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, original, 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}
	for _, command := range []string{"mcp", "ui"} {
		root := setup(t)
		if err := activateInvocationLocale([]string{"--repo", root, command}); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
		if got := textassets.ActiveLocale(); got != textassets.LegacyLocale {
			t.Fatalf("%s: locale = %q, want legacy %q", command, got, textassets.LegacyLocale)
		}
		after, err := os.ReadFile(config.FilePath(root))
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != string(original) {
			t.Fatalf("%s rewrote configuration:\n%s", command, after)
		}
	}
	// An ordinary command still records the upgrade, as the contract says.
	root := setup(t)
	if err := activateInvocationLocale([]string{"--repo", root, "status"}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) == string(original) {
		t.Fatal("ordinary command no longer records the legacy Locale")
	}
}
