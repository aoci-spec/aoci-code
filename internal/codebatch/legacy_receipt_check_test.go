package codebatch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The machine no longer derives anything from R, but receipts issued before the upgrade carry observed_relations,
// and receipt loading refuses unknown fields. This test locks that an upgrade does not deadlock a plan in progress: the old field is
// discarded once read in, and the receipt drives commit validation as usual.
func TestReceiptCarryingLegacyObservedRelationsStillLoads(t *testing.T) {
	root := t.TempDir()
	plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64), "aoci.code.txt",
		strings.Repeat("c", 64), codeCandidates(6), 4)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".aoci", "drafts", "code-cognition", "candidate-"+plan.BatchID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "observed_relations") {
		t.Fatal("新收据不应再写出关系事实")
	}
	// Simulate a pre-upgrade receipt: inject the field it would have written back then, as it was.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["observed_relations"] = []any{
		map[string]any{"source_object_ref": plan.Candidates[0].ObjectRef,
			"target_object_refs": []any{plan.Candidates[1].ObjectRef}},
	}
	legacy, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(legacy, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	receipt, err := LoadReceipt(root, plan.BatchID)
	if err != nil {
		t.Fatalf("新二进制必须能读旧收据: %v", err)
	}
	if len(receipt.Targets) != 4 || len(receipt.AllTargets) != 6 {
		t.Fatalf("旧收据的批次内容必须原样保留: %+v", receipt)
	}
	// The key: the old field takes part in no identity derivation, and the receipt still validates the candidates the host submits under the original binding.
	submissions := make([]Submission, 0, len(receipt.Targets))
	for index, target := range receipt.Targets {
		submissions = append(submissions, Submission{CandidateIndex: index + 1, ObjectRef: target.ObjectRef,
			CandidateID: target.CandidateID, SourceSHA256: target.SourceSHA256})
	}
	if _, err := ValidateSubmission(root, receipt.BatchID, receipt.CompositeIdentity, receipt.ScopePolicyIdentity,
		receipt.CodeVolumePath, receipt.CodeVolumeSHA256, submissions, false); err != nil {
		t.Fatalf("旧收据必须仍能校验提交: %v", err)
	}
}
