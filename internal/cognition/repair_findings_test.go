package cognition

import "testing"

func TestSortRepairFindingsComparesCandidateIndexNumerically(t *testing.T) {
	findings := []RepairFinding{
		{Domain: "code", Path: "same.go", Field: "A", RuleCode: "same_rule", CandidateIndex: 10},
		{Domain: "code", Path: "same.go", Field: "A", RuleCode: "same_rule", CandidateIndex: 2},
	}

	SortRepairFindings(findings)

	if findings[0].CandidateIndex != 2 || findings[1].CandidateIndex != 10 {
		t.Fatalf("candidate_index was not ordered numerically: %+v", findings)
	}
}
