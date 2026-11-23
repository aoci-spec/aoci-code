package codebatch

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// A batch receipt no longer stores the complete plan (#208), and it is
// written under a name an older binary does not read, so a downgrade sees no
// receipt and issues the batch again instead of colliding with a file it
// cannot validate. A receipt written before (candidate-<id>.json, plan
// included) still loads and is still checked against the plan it carries.
func TestReceiptHoldsTheBatchNotThePlan(t *testing.T) {
	root := t.TempDir()
	candidates := budgetCandidates(400, strings.Repeat("x", 300))
	plan, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), candidates, 5)
	if err != nil {
		t.Fatal(err)
	}
	short, err := os.ReadFile(receiptPath(root, plan.BatchID))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(short), "all_targets") || len(short) > 8<<10 {
		t.Fatalf("a 5-of-400 receipt must not carry the plan: %d bytes", len(short))
	}
	if _, err := os.Lstat(legacyReceiptPath(root, plan.BatchID)); !os.IsNotExist(err) {
		t.Fatal("nothing may be written under the name an older binary validates")
	}

	// The same batch as a pre-rc4 binary wrote it, under the old name only.
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
	if err := os.Remove(receiptPath(root, plan.BatchID)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyReceiptPath(root, plan.BatchID), append(legacy, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadReceipt(root, plan.BatchID)
	if err != nil || len(loaded.AllTargets) != 400 {
		t.Fatalf("a receipt written before must still load: %v", err)
	}
	if _, ok := resolveExactSubmissionReceipt(root, old.CompositeIdentity, old.ScopePolicyIdentity, old.CodeVolumePath,
		old.CodeVolumeSHA256, nil, false); ok {
		t.Fatal("an empty submission must not resolve a receipt")
	}
	again, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), candidates, 5)
	if err != nil || again.BatchID != plan.BatchID {
		t.Fatalf("issuing the same batch beside an old receipt must succeed: %v", err)
	}
	if err := RetireReceipt(root, plan.BatchID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{receiptPath(root, plan.BatchID), legacyReceiptPath(root, plan.BatchID)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("retiring must remove both names: %s", path)
		}
	}

	// A carried plan that does not contain a batch target is still refused.
	old.AllTargets = old.AllTargets[10:]
	tampered, _ := json.MarshalIndent(old, "", "  ")
	if err := os.WriteFile(legacyReceiptPath(root, plan.BatchID), append(tampered, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadReceipt(root, plan.BatchID); err == nil {
		t.Fatal("a carried plan that disagrees with the batch must be refused")
	}
}
