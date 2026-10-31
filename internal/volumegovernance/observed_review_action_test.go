package volumegovernance

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestObservedReviewActionDoesNotRelaxOtherBlockers(t *testing.T) {
	for _, other := range []string{"", "code_missing", "code_stale", "code_skipped", "baseline_missing",
		"recovery_pending", "scope_change_required", "cognition_budget_exceeded", "code_orphan",
		"ownership_conflict", "approval_required", "future_unknown_blocker"} {
		t.Run(other, func(t *testing.T) {
			facts := &Facts{StructureValid: true, Findings: []Finding{{Code: "observed_pending", Domain: "code"}}}
			if other != "" {
				facts.Findings = append(facts.Findings, Finding{Code: other, Domain: "code"})
			}
			finalize(facts)
			want := ResultBlocked
			if other == "" || other == "code_missing" || other == "code_stale" || other == "code_skipped" {
				want = machinecontract.ActionReviewObservedEvidence
			}
			if facts.Result != ResultBlocked || facts.GovernanceAligned || facts.NextRequiredAction != want {
				t.Fatalf("review changed the write gate or hid another blocker: %+v", facts)
			}
		})
	}
}
