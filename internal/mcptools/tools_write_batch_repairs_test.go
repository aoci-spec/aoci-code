package mcptools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One repair_required response names every candidate the model has to fix.
// Until v0.1.0-rc15 validation returned at the first failing candidate, so a
// batch with three bad Entries cost three round trips.
func TestBatchValidationReportsEveryRepairableCandidateInOneResponse(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		path := filepath.Join(root, "pkg", name+".go")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("package pkg\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	buildVolumeRepoAt(t, root, true, false, "")
	plan := maintainVolumeResult(t, root)
	if heldCandidatePaths(plan) != "pkg/a.go,pkg/b.go,pkg/c.go" || plan.CodePlan == nil {
		t.Fatalf("unexpected batch: %s", heldCandidatePaths(plan))
	}
	entries := make([]map[string]any, 0, 3)
	for _, candidate := range plan.Candidates {
		tag := "CD5T"
		if candidate.Path != "pkg/b.go" {
			tag = "ZZ"
		}
		entries = append(entries, map[string]any{"path": candidate.Path, "source_sha256": candidate.SourceSHA256,
			"candidate_id": candidate.CandidateID,
			"new_entry":    filepath.Base(candidate.Path) + "[" + tag + "]: F:Provides a fixture unit | R:- | A:- | S:-"})
	}
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"code_batch_id": plan.CodePlan.BatchID, "entries": entries})
	var result struct {
		Status     string   `json:"status"`
		RetryScope []string `json:"retry_scope"`
		Findings   []struct {
			CandidateIndex int `json:"candidate_index"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("%v:\n%s", err, output)
	}
	indices := map[int]bool{}
	for _, finding := range result.Findings {
		indices[finding.CandidateIndex] = true
	}
	if result.Status != autoStatusRepairRequired || !indices[1] || !indices[3] || indices[2] ||
		!strings.Contains(strings.Join(result.RetryScope, ","), "code:pkg/a.go") ||
		!strings.Contains(strings.Join(result.RetryScope, ","), "code:pkg/c.go") {
		t.Fatalf("every failed candidate must be reported at once: status=%s indices=%v retry=%v\n%s",
			result.Status, indices, result.RetryScope, output)
	}
}
