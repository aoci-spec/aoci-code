package cli

import (
	"testing"

	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// An orphan names Maintain only when authoring is the verdict; a blocked or
// Evidence-owing repository names its own stop first (audit G5-07).
func TestGovernanceNextStepRanksOrphansWithAuthoring(t *testing.T) {
	orphan := []volumegovernance.Finding{{Code: "code_orphan", Target: "gone.go"}}
	blocked := &volumegovernance.Facts{Result: volumegovernance.ResultBlocked, Findings: orphan}
	if got := volumeGovernanceNextStep(blocked); got != cliMessage("governance.next.blocked") {
		t.Fatalf("blocked with an orphan: %q", got)
	}
	evidence := &volumegovernance.Facts{Result: volumegovernance.ResultEvidenceRequired, Findings: orphan}
	if got := volumeGovernanceNextStep(evidence); got != cliMessage("governance.next.evidence_required") {
		t.Fatalf("Evidence owed with an orphan: %q", got)
	}
	authoring := &volumegovernance.Facts{Result: volumegovernance.ResultAuthoringRequired, Findings: orphan}
	if got := volumeGovernanceNextStep(authoring); got != cliMessage("governance.next.code_orphan") {
		t.Fatalf("authoring with an orphan: %q", got)
	}
}
