package cognitionoptimization

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestSelectIsDeterministicAndPreservesCompleteEntries(t *testing.T) {
	policy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	entries := []AlignedEntry{
		alignedEntry("code:normal.go", 9, strings.Repeat("r", 30), "-"),
		alignedEntry("code:target.go", 8, strings.Repeat("r", 90*3), "-"),
		alignedEntry("code:max.go", 3, strings.Repeat("r", 70*3), "-"),
	}
	first, err := Select(entries, policy, SelectOptions{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	reversed := []AlignedEntry{entries[2], entries[1], entries[0]}
	second, err := Select(reversed, policy, SelectOptions{MaxEntries: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("selection depends on input order:\nfirst=%#v\nsecond=%#v", first, second)
	}
	if got := []string{first.Batch[0].ObjectRef, first.Batch[1].ObjectRef, first.RemainingObjectRefs[0]}; !reflect.DeepEqual(got, []string{"code:max.go", "code:target.go", "code:normal.go"}) {
		t.Fatalf("unexpected stable priority: %v", got)
	}
	maxCandidate := first.Batch[0]
	if maxCandidate.Importance != 3 || maxCandidate.Cost.RTokens != 70 || maxCandidate.RTargetTokens != 25 ||
		maxCandidate.RMaxTokens != 50 || maxCandidate.ExistingEntry != entries[2].ExistingEntry {
		t.Fatalf("candidate measurements or semantics changed: %#v", maxCandidate)
	}
}

func TestSelectExplicitScopeAndUnknownRefs(t *testing.T) {
	policy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	entries := []AlignedEntry{
		alignedEntry("code:a.go", 5, "-", "-"),
		alignedEntry("code:b.go", 9, "-", "-"),
	}
	selection, err := Select(entries, policy, SelectOptions{ObjectRefs: []string{"code:a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if selection.TotalTargets != 1 || len(selection.Batch) != 1 || selection.Batch[0].ObjectRef != "code:a.go" || len(selection.RemainingObjectRefs) != 0 {
		t.Fatalf("explicit selection mismatch: %#v", selection)
	}
	if _, err := Select(entries, policy, SelectOptions{ObjectRefs: []string{"code:missing.go"}}); err == nil {
		t.Fatal("unknown explicit object_ref was accepted")
	}
	if _, err := Select(entries, policy, SelectOptions{ObjectRefs: []string{"code:a.go", "code:a.go"}}); err == nil {
		t.Fatal("duplicate explicit object_ref was accepted")
	}
}

func TestSelectNeverExceedsExistingBatchBoundary(t *testing.T) {
	policy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	entries := make([]AlignedEntry, 0, 205)
	for index := 0; index < 205; index++ {
		entries = append(entries, alignedEntry(fmt.Sprintf("code:%03d.go", index), 5, "-", "-"))
	}
	selection, err := Select(entries, policy, SelectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if selection.TotalTargets != 205 || len(selection.Batch) != MaxBatchEntries || len(selection.RemainingObjectRefs) != 5 {
		t.Fatalf("batch boundary mismatch: %#v", selection)
	}
	if _, err := Select(entries, policy, SelectOptions{MaxEntries: MaxBatchEntries + 1}); err == nil {
		t.Fatal("oversized optimization batch was accepted")
	}
}

func TestSelectFailsClosedOnInvalidAlignedEntry(t *testing.T) {
	policy := cognitionbudget.DefaultPolicy(machinecontract.BudgetModeObserve)
	invalidC := alignedEntry("code:a.go", 5, "-", "-")
	invalidC.ExistingEntry = "a.go[INVALID]: F:x | R:- | A:- | S:-"
	if _, err := Select([]AlignedEntry{invalidC}, policy, SelectOptions{}); err == nil {
		t.Fatal("invalid C importance was accepted")
	}
	invalidSource := alignedEntry("code:a.go", 5, "-", "-")
	invalidSource.SourceSHA256 = "bad"
	if _, err := Select([]AlignedEntry{invalidSource}, policy, SelectOptions{}); err == nil {
		t.Fatal("invalid source binding was accepted")
	}
}

func alignedEntry(objectRef string, importance int, relation, constraint string) AlignedEntry {
	path := strings.TrimPrefix(objectRef, "code:")
	line := fmt.Sprintf("%s[CD%dS]: F:fixture responsibility | R:%s | A:- | S:%s", path, importance, relation, constraint)
	return AlignedEntry{ObjectRef: objectRef, Path: path, SourceSHA256: fmt.Sprintf("%064x", importance+len(path)), ExistingEntry: line}
}
