package cognitionoptimization

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognitionbudget"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// An E scale mismatch ranks an Entry for review after S absence on a
// high-importance Entry and before plain importance order; it is a fact the
// caller supplies, never a judgement the selector makes.
func TestSelectRanksEScaleMismatchAfterSAbsence(t *testing.T) {
	policy, err := cognitionbudget.Normalize(cognitionbudget.DefaultPolicy(machinecontract.BudgetModeEnforce))
	if err != nil {
		t.Fatal(err)
	}
	entries := []AlignedEntry{
		{ObjectRef: "code:a.go", Path: "a.go", SourceSHA256: strings.Repeat("1", 64), ExistingEntry: "a.go[CG5T]: F:Plain | R:- | A:- | S:kept"},
		{ObjectRef: "code:b.go", Path: "b.go", SourceSHA256: strings.Repeat("2", 64), ExistingEntry: "b.go[CG5T]: F:Wrong scale | R:- | A:- | S:kept", EScaleMismatch: true},
		{ObjectRef: "code:c.go", Path: "c.go", SourceSHA256: strings.Repeat("3", 64), ExistingEntry: "c.go[CG7T]: F:No S | R:- | A:- | S:-"},
	}
	selection, err := Select(entries, policy, SelectOptions{MaxEntries: 3})
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, candidate := range selection.Batch {
		got = append(got, candidate.ObjectRef)
	}
	if len(got) != 3 || got[0] != "code:c.go" || got[1] != "code:b.go" || got[2] != "code:a.go" {
		t.Fatalf("unexpected review order: %v", got)
	}
	if !selection.Batch[1].EScaleMismatchFact || selection.Batch[0].EScaleMismatchFact {
		t.Fatalf("the E scale fact must travel with its candidate: %+v", selection.Batch)
	}
}
