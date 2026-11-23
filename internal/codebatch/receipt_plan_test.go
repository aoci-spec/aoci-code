package codebatch

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// A batch receipt no longer stores the complete plan (#208): its size follows
// the batch, not the repository. A receipt written before that still loads
// and is checked against the plan it carries, and issuing the same batch
// again rewrites it in the short form instead of refusing a conflict.
func TestReceiptHoldsTheBatchNotThePlan(t *testing.T) {
	root := t.TempDir()
	candidates := budgetCandidates(400, strings.Repeat("x", 300))
	plan, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), candidates, 5)
	if err != nil {
		t.Fatal(err)
	}
	path := receiptPath(root, plan.BatchID)
	short, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(short), "all_targets") || len(short) > 8<<10 {
		t.Fatalf("a 5-of-400 receipt must not carry the plan: %d bytes", len(short))
	}

	// The same batch as a pre-rc4 binary wrote it.
	var old Receipt
	if err := json.Unmarshal(short, &old); err != nil {
		t.Fatal(err)
	}
	ordered, _ := Select(candidates, 5)
	old.AllTargets = targetsWithoutCandidateIDs(ordered)
	legacy, err := json.MarshalIndent(old, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(legacy, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReceipt(root, plan.BatchID)
	if err != nil || len(loaded.AllTargets) != 400 {
		t.Fatalf("a receipt carrying its plan must still load: %v", err)
	}
	again, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), candidates, 5)
	if err != nil || again.BatchID != plan.BatchID {
		t.Fatalf("issuing the same batch over an old receipt must succeed: %v", err)
	}
	if rewritten, _ := os.ReadFile(path); string(rewritten) != string(short) {
		t.Fatal("the old receipt must be rewritten in the short form")
	}

	// A carried plan that does not contain a batch target is still refused.
	old.AllTargets = old.AllTargets[10:]
	tampered, _ := json.MarshalIndent(old, "", "  ")
	if err := os.WriteFile(path, append(tampered, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReceipt(root, plan.BatchID); err == nil {
		t.Fatal("a carried plan that disagrees with the batch must be refused")
	}
}

func TestIssuingABatchPrunesOldCandidateReceipts(t *testing.T) {
	root := t.TempDir()
	first, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), budgetCandidates(6, ""), 3)
	if err != nil {
		t.Fatal(err)
	}
	aged := time.Now().Add(-15 * 24 * time.Hour)
	if err := os.Chtimes(receiptPath(root, first.BatchID), aged, aged); err != nil {
		t.Fatal(err)
	}
	second, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("4"), budgetCandidates(6, ""), 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReceipt(root, first.BatchID); err == nil || !strings.Contains(err.Error(), "code_candidate_receipt_missing") {
		t.Fatalf("a receipt past its retention must be pruned: %v", err)
	}
	if _, err := LoadReceipt(root, second.BatchID); err != nil {
		t.Fatalf("the batch just issued must stay: %v", err)
	}
}
