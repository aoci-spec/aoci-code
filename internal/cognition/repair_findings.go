package cognition

import "sort"

// SortRepairFindings orders candidate diagnostics by the public Repair Finding
// contract. CandidateIndex is compared as an integer and remains the immutable
// 1-based position in the caller's complete batch.
func SortRepairFindings(findings []RepairFinding) {
	sort.SliceStable(findings, func(i, j int) bool {
		left, right := findings[i], findings[j]
		if left.Domain != right.Domain {
			return left.Domain < right.Domain
		}
		if left.Path != right.Path {
			return left.Path < right.Path
		}
		if left.Field != right.Field {
			return left.Field < right.Field
		}
		if left.RuleCode != right.RuleCode {
			return left.RuleCode < right.RuleCode
		}
		return left.CandidateIndex < right.CandidateIndex
	})
}
