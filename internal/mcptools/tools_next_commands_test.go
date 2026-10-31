package mcptools

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

// The commands must be runnable as returned: absolute quoted executable plus a
// suffix that is byte-identical to the shared machinecontract spelling, so the
// MCP surface and the CLI Guide can never drift apart on one command.
func TestNextCommandsCarryTheSharedSpellings(t *testing.T) {
	prefix := nextCommandPrefix()
	if prefix == "" {
		t.Fatal("the test binary has a resolvable executable; prefix must not be empty")
	}
	if !strings.HasPrefix(prefix, "'") && !strings.HasPrefix(prefix, `"`) {
		t.Fatalf("prefix must be quoted for the shell: %q", prefix)
	}

	proof := finalProofNextCommands()
	if len(proof) != 3 ||
		!strings.HasSuffix(proof[0], " "+machinecontract.RemediationCommandVerify) ||
		!strings.HasSuffix(proof[1], " "+machinecontract.RemediationCommandAggregateCheck) ||
		!strings.HasSuffix(proof[2], " "+machinecontract.RemediationCommandAgentGuide) {
		t.Fatalf("final proof must name verify, aggregate check, and guide in order: %v", proof)
	}

	facts := &volumegovernance.Facts{}
	facts.Findings = []volumegovernance.Finding{
		{Code: "observed_pending"},
		{Code: "observed_pending"}, // duplicates must not duplicate commands
		{Code: "cognition_budget_exceeded"},
	}
	blocked := blockedNextCommands(facts)
	joined := strings.Join(blocked, "\n")
	if len(blocked) != 2 ||
		strings.Contains(joined, machinecontract.RemediationCommandScopeAcknowledge) ||
		!strings.Contains(joined, machinecontract.RemediationCommandScopeBudgetSet) ||
		strings.Count(joined, machinecontract.RemediationCommandScopeStatus+"\n")+strings.Count(joined, machinecontract.RemediationCommandScopeStatus) < 1 {
		t.Fatalf("blocked commands wrong: %v", blocked)
	}
	review := blockedNextCommands(&volumegovernance.Facts{
		NextRequiredAction: machinecontract.ActionReviewObservedEvidence,
		Findings:           []volumegovernance.Finding{{Code: "observed_pending"}, {Code: "observed_pending"}},
	})
	if len(review) != 3 || !strings.HasSuffix(review[0], machinecontract.RemediationCommandScopeStatus) ||
		!strings.HasSuffix(review[1], machinecontract.RemediationCommandScopeAcknowledge) ||
		!strings.HasSuffix(review[2], machinecontract.RemediationCommandAgentGuide) {
		t.Fatalf("observe review must lead through status, reviewed acknowledgement, and a fresh Guide: %v", review)
	}

	scopeCommands := blockedNextCommands(&volumegovernance.Facts{Findings: []volumegovernance.Finding{
		{Code: "scope_change_required"}, {Code: "scope_change_required"},
	}})
	if len(scopeCommands) != 2 ||
		!strings.HasSuffix(scopeCommands[0], " "+machinecontract.RemediationCommandScopeStatus) ||
		!strings.HasSuffix(scopeCommands[1], " "+machinecontract.RemediationCommandScopeActivate) {
		t.Fatalf("scope blocker must name status then activation once: %v", scopeCommands)
	}

	if commands := blockedNextCommands(&volumegovernance.Facts{}); commands != nil {
		t.Fatalf("no clearable blocker must yield no commands, got %v", commands)
	}

	evidence := evidenceNextCommands()
	if len(evidence) != 2 ||
		!strings.HasSuffix(evidence[0], " "+machinecontract.RemediationCommandDatabaseSnapshot) ||
		!strings.HasSuffix(evidence[1], " "+machinecontract.RemediationCommandDatabaseBaselineAccept) {
		t.Fatalf("evidence chain wrong: %v", evidence)
	}
}

// A blocked Maintain names its actual blocker. The orphan token is kept for the
// one case it was right about; a pending Observe review alone must not send a
// Host hunting for orphans that do not exist (#147).
func TestBlockedNextActionNamesTheActualBlocker(t *testing.T) {
	observed := &volumegovernance.Facts{NextRequiredAction: machinecontract.ActionReviewObservedEvidence, Findings: []volumegovernance.Finding{{Code: "observed_pending"}}}
	if got := blockedNextAction(observed, nil); got != machinecontract.ActionReviewObservedEvidence {
		t.Fatalf("pending Observe review named %q", got)
	}
	if got := blockedNextAction(observed, []string{"code:gone.go"}); got != machinecontract.MaintainActionExplicitOrphanRemove {
		t.Fatalf("an orphan must keep the orphan token, got %q", got)
	}
	for _, step := range []struct {
		codes []string
		want  string
	}{
		{[]string{"observed_pending", "recovery_pending"}, machinecontract.MaintainActionClosePendingRecovery},
		{[]string{"cognition_budget_exceeded", "baseline_missing"}, machinecontract.MaintainActionScanToEstablishBaseline},
		{[]string{"observed_pending", "scope_change_required"}, machinecontract.MaintainActionActivateScopeChange},
		{[]string{"cognition_budget_exceeded"}, machinecontract.MaintainActionResolveCognitionBudget},
		{[]string{"observed_pending", "cognition_budget_exceeded"}, machinecontract.MaintainActionResolveCognitionBudget},
		{[]string{"observed_pending", "future_unknown_blocker"}, machinecontract.MaintainActionResolveBlocker},
		{[]string{"code_skipped"}, machinecontract.MaintainActionResolveBlocker},
	} {
		facts := &volumegovernance.Facts{}
		for _, code := range step.codes {
			facts.Findings = append(facts.Findings, volumegovernance.Finding{Code: code})
		}
		if got := blockedNextAction(facts, nil); got != step.want {
			t.Fatalf("%v named %q, want %q", step.codes, got, step.want)
		}
	}
	if got := blockedNextAction(nil, nil); got != machinecontract.MaintainActionResolveBlocker {
		t.Fatalf("no facts must still answer a non-orphan token, got %q", got)
	}
}
