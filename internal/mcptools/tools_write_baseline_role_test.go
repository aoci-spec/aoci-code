package mcptools

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// A batch that advances the Baseline under Managed Scope keeps every
// fingerprint's index role. Until v0.1.0-rc15 the batch path wrote roleless
// fingerprints over the ones scan had stamped.
func TestBatchApplyKeepsManagedScopeRolesInTheBaseline(t *testing.T) {
	root := buildVolumeRepo(t, true, false)
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "util.go"), []byte("package pkg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	enableProductionManagedScope(t, root)
	before, _, err := baseline.Load(root)
	if err != nil || before.ManagedScope == nil || before.Files["pkg/util.go"].Role != machinecontract.ScopeRoleIndex {
		t.Fatalf("fixture must start from a managed Baseline with roles: err=%v files=%+v", err, before.Files)
	}
	plan := maintainVolumeResult(t, root)
	if heldCandidatePaths(plan) != "pkg/util.go" || plan.CodePlan == nil {
		t.Fatalf("unexpected batch: %s", heldCandidatePaths(plan))
	}
	candidate := plan.Candidates[0]
	output := callVolumeTool(t, connectMCPClient(t, root), "aoci_update_entry", map[string]any{
		"code_batch_id": plan.CodePlan.BatchID,
		"entries": []map[string]any{{"path": candidate.Path, "source_sha256": candidate.SourceSHA256,
			"candidate_id": candidate.CandidateID, "new_entry": "util.go[CD5T]: F:Provides fixture helpers | R:- | A:- | S:-"}}})
	if !contains(output, `"status":"applied"`) {
		t.Fatalf("batch did not apply:\n%s", output)
	}
	after, _, err := baseline.Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"pkg/util.go", "aoci.code.txt"} {
		if after.Files[rel].Role != machinecontract.ScopeRoleIndex {
			t.Fatalf("%s lost its role after the batch: %+v", rel, after.Files[rel])
		}
	}
}

func contains(text, want string) bool {
	return len(want) == 0 || (len(text) >= len(want) && indexOf(text, want) >= 0)
}

func indexOf(text, want string) int {
	for i := 0; i+len(want) <= len(text); i++ {
		if text[i:i+len(want)] == want {
			return i
		}
	}
	return -1
}
