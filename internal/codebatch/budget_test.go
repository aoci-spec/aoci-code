package codebatch

import (
	"fmt"
	"testing"
)

func budgetCandidates(count int, existing string) []Candidate {
	values := make([]Candidate, 0, count)
	for index := count - 1; index >= 0; index-- {
		path := fmt.Sprintf("pkg/file_%03d.go", index)
		values = append(values, Candidate{Target: Target{Change: "create", ObjectRef: "code:" + path, Path: path,
			SourceSHA256: fmt.Sprintf("%064d", index), ExistingEntry: existing}})
	}
	return values
}

func hexIdentity(seed string) string {
	return fmt.Sprintf("%064s", seed)
}

func fixedSize(size int) Budget {
	return Budget{Bytes: 1000, Size: func(Candidate) int { return size }}
}

// The byte bound cuts the ordered prefix where the next candidate would
// exceed it, keeps at least one, never exceeds the count cap, and depends
// only on the candidates, so the same input always yields the same batch.
func TestSelectWithBudgetCutsTheOrderedPrefixDeterministically(t *testing.T) {
	candidates := budgetCandidates(30, "")
	ordered, selected := SelectWithBudget(candidates, 50, fixedSize(100))
	if len(ordered) != 30 || len(selected) != 10 || selected[0].Path != "pkg/file_000.go" || selected[9].Path != "pkg/file_009.go" {
		t.Fatalf("budget of 1000 over 100-byte candidates must select the first ten in path order: %d %v", len(selected), selected)
	}
	if _, again := SelectWithBudget(candidates, 50, fixedSize(100)); len(again) != 10 || again[9].Path != selected[9].Path {
		t.Fatalf("selection is not deterministic: %v", again)
	}
	if _, capped := SelectWithBudget(candidates, 4, fixedSize(100)); len(capped) != 4 {
		t.Fatalf("the count cap still bounds a batch under budget: %d", len(capped))
	}
	if _, single := SelectWithBudget(candidates, 50, fixedSize(5000)); len(single) != 1 {
		t.Fatalf("an oversize candidate must still ship alone: %d", len(single))
	}
	if _, unbounded := SelectWithBudget(candidates, 50, Budget{}); len(unbounded) != 30 {
		t.Fatalf("a zero budget bounds by count alone: %d", len(unbounded))
	}
	if _, exact := SelectWithBudget(candidates, 50, Budget{Bytes: 300, Size: func(Candidate) int { return 100 }}); len(exact) != 3 {
		t.Fatalf("a batch that exactly fills the budget keeps every candidate: %d", len(exact))
	}
}

func TestCountBatchesCoversEveryCandidate(t *testing.T) {
	candidates := budgetCandidates(45, "")
	if got := CountBatches(candidates, 50, fixedSize(100)); got != 5 {
		t.Fatalf("45 candidates at ten per batch take five batches, got %d", got)
	}
	if got := CountBatches(candidates, 20, Budget{}); got != 3 {
		t.Fatalf("count-only budget: 45 at 20 per batch take three, got %d", got)
	}
	if got := CountBatches(nil, 20, fixedSize(100)); got != 0 {
		t.Fatalf("no candidates take no batches, got %d", got)
	}
}

func TestBuildPlanWithBudgetReportsTheTeamBudgetAndKeepsIdentities(t *testing.T) {
	root := t.TempDir()
	candidates := budgetCandidates(12, "")
	budget := Budget{Bytes: 500, Size: func(Candidate) int { return 100 }, Total: 24 << 10}
	plan, err := BuildPlanWithBudget(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), candidates, 50, budget)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Included != 5 || plan.Remaining != 7 || plan.MaxEntries != 50 || plan.TransportBudgetBytes != 24<<10 {
		t.Fatalf("budgeted plan facts: included=%d remaining=%d max=%d budget=%d", plan.Included, plan.Remaining, plan.MaxEntries, plan.TransportBudgetBytes)
	}
	receipt, err := LoadReceipt(root, plan.BatchID)
	if err != nil || len(receipt.Targets) != 5 || len(receipt.AllTargets) != 12 {
		t.Fatalf("the disk receipt must hold the budgeted batch and the complete plan: %v %d %d", err, len(receipt.Targets), len(receipt.AllTargets))
	}
	unbudgeted, err := BuildPlan(root, hexIdentity("1"), hexIdentity("2"), "aoci.code.txt", hexIdentity("3"), candidates, 50)
	if err != nil || unbudgeted.PlanID != plan.PlanID {
		t.Fatalf("the byte bound changes the batch, never the plan identity: %v %s %s", err, unbudgeted.PlanID, plan.PlanID)
	}
}
