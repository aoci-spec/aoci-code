package mcptools

import (
	"os"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/internal/testkit"
	"github.com/aoci-spec/aoci-code/textassets"
)

// TestServerReadsConfigurationWithoutWritingIt pins that the MCP server never
// writes the legacy Locale upgrade. A Legacy repository with no team
// configuration, or with one that predates the Locale field, is served as
// zh-CN either way; a write here would drift the configuration bytes that a
// migration envelope or Scope preview prepared before the server started is
// bound to.
func TestServerReadsConfigurationWithoutWritingIt(t *testing.T) {
	testkit.PinLocale(t, textassets.DefaultLocale)
	root := t.TempDir()
	testkit.WriteFile(t, root, "aoci.txt", "# legacy\n")
	serve := func() {
		t.Helper()
		if _, err := NewServer(root, "config-read-only-test"); err != nil {
			t.Fatal(err)
		}
		_, _ = loadCognitionCtx(root)
		appendWriteFailEvent(root, "update_entries_batch", ledger.SourceAgent, &Fail{Code: errBadArgs, Msg: "probe"}, time.Now())
		appendAutoFinalizeEvent(root, autoStatusRepairRequired, autoMetrics{})
	}

	serve()
	if got := textassets.ActiveLocale(); got != textassets.LegacyLocale {
		t.Fatalf("server Locale = %q, want the legacy %q", got, textassets.LegacyLocale)
	}
	if _, err := os.Lstat(config.FilePath(root)); !os.IsNotExist(err) {
		t.Fatalf("the server wrote a team configuration: %v", err)
	}

	original := "{\"version\":1,\"index_path\":\"aoci.txt\"}\n"
	testkit.WriteFile(t, root, ".aoci/config.json", original)
	serve()
	after, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Fatalf("the server rewrote a pre-Locale configuration:\n%s", after)
	}
}
