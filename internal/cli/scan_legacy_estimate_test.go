package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// A Legacy index keeps its Entries on the Root asset; a re-scan of such a
// repository must not count every described file as a first-build target.
func TestScanEstimateCountsNoDescribedLegacyEntry(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	index := "===" + filepath.ToSlash(root) + "/src/===\na.go[CG5T]: F:Provides the legacy fixture | R:- | A:- | S:-\n"
	if err := os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	snap, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, baseline.NewBaseline(snap)); err != nil {
		t.Fatal(err)
	}
	code, out := runHeldSourcesCLI(t, root, "scan", "--dry-run", "--json")
	if code != ExitOK {
		t.Fatalf("scan: code=%d\n%s", code, out)
	}
	var scan struct {
		Estimate struct {
			Targets int `json:"targets"`
			Rounds  int `json:"rounds"`
		} `json:"authoring_estimate"`
	}
	if err := json.Unmarshal([]byte(out), &scan); err != nil {
		t.Fatalf("scan --json: %v\n%s", err, out)
	}
	if scan.Estimate.Targets != 0 || scan.Estimate.Rounds != 0 {
		t.Fatalf("a described Legacy Entry is not a first-build target: %+v", scan.Estimate)
	}
}
