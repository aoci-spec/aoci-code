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

// 装箱只看确定性顺序与上限, 不看条目语义。宿主在上一批里把 R 指到本批之外
// 的任何地方都不改变下一批的构成 —— 关系是模型写给模型的标注, 不是机器的排程
// 约束, 也就永远不可能因为"关系闭不上"而卡住整个索引的建立。
func TestCodeBatchesAreAPureOrderedPrefixIndependentOfEntrySemantics(t *testing.T) {
	root := t.TempDir()
	candidates := codeCandidates(201)
	candidates[0].ExistingEntry = "0000.go[XAA9T]: F:old | R:code:0200.go | A:- | S:-"
	plan, err := BuildPlan(root, strings.Repeat("a", 64), strings.Repeat("b", 64), "aoci.code.txt",
		strings.Repeat("c", 64), candidates, 200)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Included != 200 || plan.Remaining != 1 || !plan.ContinuationRequired {
		t.Fatalf("批次事实不对: %#v", plan)
	}
	for index, candidate := range plan.Candidates {
		if want := fmt.Sprintf("code:%04d.go", index); candidate.ObjectRef != want {
			t.Fatalf("批次不是确定序前缀: [%d]=%s want=%s", index, candidate.ObjectRef, want)
		}
	}
	// 已存在条目的原文照常随批下发, 供宿主做增量创作。
	if plan.Candidates[0].ExistingEntry == "" {
		t.Fatal("既有条目原文丢失")
	}
	// 关系指向的对象落在下一批: 这完全合法, 本批照常可提交完成。
	submissions := make([]Submission, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		submissions = append(submissions, Submission{ObjectRef: candidate.ObjectRef,
			CandidateID: candidate.CandidateID, SourceSHA256: candidate.SourceSHA256})
	}
	if _, err := ValidateSubmission(root, plan.BatchID, plan.CompositeIdentity, plan.ScopePolicyIdentity,
		plan.CodeVolumePath, plan.CodeVolumeSHA256, submissions, false); err != nil {
		t.Fatalf("跨批关系不应影响提交: %v", err)
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
