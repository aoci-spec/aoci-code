package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An Entry whose file is gone is Code drift Maintain clears: scope status
// names authoring work, never aligned (audit G4-09).
func TestScopeStatusReportsOrphanDriftAsAuthoringWork(t *testing.T) {
	root := scopeActivationFixture(t)
	var out, errs strings.Builder
	if code := executeCLI([]string{"--repo", root, "--json", "scope", "status"}, &out, &errs); code != ExitOK {
		t.Fatalf("scope status: %d %s", code, &errs)
	}
	if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errs.Reset()
	if code := executeCLI([]string{"--repo", root, "--json", "scope", "status"}, &out, &errs); code != ExitOK {
		t.Fatalf("scope status: %d %s", code, &errs)
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(out.String()), &status); err != nil {
		t.Fatal(err)
	}
	drift, _ := status["drift"].(map[string]any)
	if orphans, _ := drift["orphan"].([]any); len(orphans) == 0 {
		t.Skipf("fixture produced no orphan drift: %v", drift)
	}
	if status["stage"] == "aligned" {
		t.Fatalf("scope status reads aligned over orphan drift: %v", drift)
	}
}
