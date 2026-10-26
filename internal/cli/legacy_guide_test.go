package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// Since 0.2.0 the Legacy single-file layout gets one blocked Guide: its stage
// is the read-only token, its next action the migration, and its commands
// carry the onboard command. A pending transaction receipt is named with its
// closure, because the onboard command refuses while one is pending.
func TestLegacyGuideIsBlockedOnMigrationAndNamesPendingReceipts(t *testing.T) {
	root, _ := buildManualAtomicEntriesRepo(t)
	out, stderr, code := runCLI(t, root, "--json", "index", "agent", "guide", "--agent", "codex")
	if code != ExitOK {
		t.Fatalf("guide: %d %s", code, stderr)
	}
	var guide volumeAgentGuide
	if err := json.Unmarshal([]byte(out), &guide); err != nil {
		t.Fatalf("guide JSON: %v\n%s", err, out)
	}
	if guide.Mode != agentGuideModeBlocked || guide.Stage != machinecontract.StatusLegacyReadOnlyMigrateRequired ||
		guide.NextAction != machinecontract.MaintainActionMigrateLegacyRepository || guide.Complete ||
		!strings.Contains(guide.Commands.Onboard, machinecontract.RemediationCommandCognitionOnboardStart) ||
		len(guide.Findings) != 1 || guide.Findings[0].Code != machinecontract.StatusLegacyReadOnlyMigrateRequired {
		t.Fatalf("legacy guide contract: %+v", guide)
	}

	directory := filepath.Join(root, ".aoci", "transactions")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "header-cafe.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, stderr, code = runCLI(t, root, "--json", "index", "agent", "guide", "--agent", "codex")
	if code != ExitOK {
		t.Fatalf("guide with a pending receipt: %d %s", code, stderr)
	}
	guide = volumeAgentGuide{}
	if err := json.Unmarshal([]byte(out), &guide); err != nil {
		t.Fatalf("guide JSON: %v\n%s", err, out)
	}
	instructions := strings.Join(guide.Instructions, "\n")
	if len(guide.Findings) != 2 || guide.Findings[1].Code != "recovery_pending" ||
		!strings.Contains(guide.Findings[1].Target, "header-cafe.json") ||
		!strings.Contains(instructions, "header-cafe.json") || !strings.Contains(instructions, "0.1.x") {
		t.Fatalf("pending receipt not named with its closure: %+v", guide)
	}
}
