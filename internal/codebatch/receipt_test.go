package codebatch

import (
	"fmt"
	"strings"
	"testing"
)

func TestLargeCodeCandidatePlansUseCompleteBoundedBatches(t *testing.T) {
	for _, test := range []struct {
		total int
		want  []int
	}{{199, []int{199}}, {200, []int{200}}, {201, []int{200, 1}},
		{394, []int{200, 194}}, {640, []int{200, 200, 200, 40}}} {
		t.Run(fmt.Sprint(test.total), func(t *testing.T) {
			root := t.TempDir()
			remaining := codeCandidates(test.total)
			got := []int{}
			for len(remaining) > 0 {
				plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64),
					"aoci.code.txt", strings.Repeat("c", 64), remaining, 200)
				if err != nil {
					t.Fatal(err)
				}
				if plan.Included > 200 || !plan.CompleteCandidateSetForBatch || plan.Remaining != len(remaining)-plan.Included ||
					plan.ContinuationRequired != (plan.Remaining > 0) {
					t.Fatalf("invalid batch facts: %#v", plan)
				}
				submissions := make([]Submission, 0, len(plan.Candidates))
				for _, candidate := range plan.Candidates {
					submissions = append(submissions, Submission{ObjectRef: candidate.ObjectRef,
						CandidateID: candidate.CandidateID, SourceSHA256: candidate.SourceSHA256})
				}
				if _, err := ValidateSubmission(root, plan.BatchID, plan.CompositeIdentity, plan.ScopePolicyIdentity,
					plan.CodeVolumePath, plan.CodeVolumeSHA256, submissions, false); err != nil {
					t.Fatal(err)
				}
				got = append(got, plan.Included)
				remaining = remaining[plan.Included:]
			}
			if fmt.Sprint(got) != fmt.Sprint(test.want) {
				t.Fatalf("batches=%v want=%v", got, test.want)
			}
		})
	}
}

func TestCodeCandidateReceiptRejectsPartialAndStaleSubmission(t *testing.T) {
	root := t.TempDir()
	plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64), "aoci.code.txt",
		strings.Repeat("c", 64), codeCandidates(201), 200)
	if err != nil {
		t.Fatal(err)
	}
	submissions := make([]Submission, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		submissions = append(submissions, Submission{ObjectRef: candidate.ObjectRef,
			CandidateID: candidate.CandidateID, SourceSHA256: candidate.SourceSHA256})
	}
	if _, err := ValidateSubmission(root, plan.BatchID, plan.CompositeIdentity, plan.ScopePolicyIdentity,
		plan.CodeVolumePath, plan.CodeVolumeSHA256, submissions[:199], false); err == nil {
		t.Fatal("partial current batch was accepted")
	}
	if _, err := ValidateSubmission(root, plan.BatchID, strings.Repeat("d", 64), plan.ScopePolicyIdentity,
		plan.CodeVolumePath, plan.CodeVolumeSHA256, submissions, false); err == nil {
		t.Fatal("stale composite identity was accepted")
	}
}

func TestRelationReplanKeepsPendingTargetsAndExistingSemantics(t *testing.T) {
	root := t.TempDir()
	candidates := codeCandidates(201)
	candidates[0].ExistingEntry = "0000.go[XAA9T]: F:old | R:- | A:- | S:-"
	plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64), "aoci.code.txt",
		strings.Repeat("c", 64), candidates, 200)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := LoadReceipt(root, plan.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	observed := []ObservedRelation{{SourceObjectRef: "code:0000.go", TargetObjectRefs: []string{"code:0200.go"}}}
	replanned, _, err := ReplanForRelations(root, receipt, observed, nil, 200)
	if err != nil {
		t.Fatal(err)
	}
	seen, preserved := map[string]bool{}, false
	for _, candidate := range replanned.Candidates {
		seen[candidate.ObjectRef] = true
		if candidate.ObjectRef == "code:0000.go" && candidate.ExistingEntry != "" {
			preserved = true
		}
	}
	if !seen["code:0000.go"] || !seen["code:0200.go"] || !preserved || replanned.Included != 200 {
		t.Fatalf("relation closure was not preserved: %#v", replanned)
	}
}

func codeCandidates(count int) []Candidate {
	result := make([]Candidate, 0, count)
	for index := 0; index < count; index++ {
		path := fmt.Sprintf("%04d.go", index)
		result = append(result, Candidate{Target: Target{ObjectRef: "code:" + path, Path: path,
			Change: "create", SourceSHA256: fmt.Sprintf("%064x", index+1)}})
	}
	return result
}
