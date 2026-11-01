package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

// A Guide at evidence_required used to name the stage and stop there, while
// Maintain at the same state returned the snapshot and accept commands. The
// Guide now carries both, bound to the running binary, and names the source
// that owes Evidence (2026-11-02).
func TestGuideAtEvidenceRequiredNamesTheSnapshotCommands(t *testing.T) {
	pinDefaultLocale(t)
	root := scopeActivationFixture(t)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "local", Engine: dbevidence.EngineSQLite, Path: "app.db", Enabled: true}}
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, _ := runCLI(t, root, "--json", "index", "agent", "guide", "--agent", "claude")
	var guide struct {
		Stage    string `json:"stage"`
		Commands struct {
			DatabaseSnapshot       string `json:"database_snapshot"`
			DatabaseBaselineAccept string `json:"database_baseline_accept"`
		} `json:"commands"`
		Instructions []string `json:"instructions"`
	}
	if err := json.Unmarshal([]byte(stdout), &guide); err != nil {
		t.Fatalf("guide JSON: %v\n%s%s", err, stdout, stderr)
	}
	if guide.Stage != "evidence_required" {
		t.Fatalf("fixture did not reach evidence_required: %s", stdout)
	}
	if !strings.Contains(guide.Commands.DatabaseSnapshot, "database snapshot --source {source} --json") ||
		!strings.Contains(guide.Commands.DatabaseBaselineAccept, "database baseline accept --source {source} --snapshot-sha {snapshot_sha} --json") ||
		strings.HasPrefix(guide.Commands.DatabaseSnapshot, "aoci ") {
		t.Fatalf("the Guide does not carry the bound snapshot and accept commands: %+v", guide.Commands)
	}
	if !strings.Contains(strings.Join(guide.Instructions, "\n"), "owes catalog Evidence: local") {
		t.Fatalf("the Guide does not name the source that owes Evidence: %v", guide.Instructions)
	}
}
