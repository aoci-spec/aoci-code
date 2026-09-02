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
	if len(blocked) != 3 ||
		!strings.Contains(joined, machinecontract.RemediationCommandScopeAcknowledge) ||
		!strings.Contains(joined, machinecontract.RemediationCommandScopeBudgetSet) ||
		strings.Count(joined, machinecontract.RemediationCommandScopeStatus+"\n")+strings.Count(joined, machinecontract.RemediationCommandScopeStatus) < 1 {
		t.Fatalf("blocked commands wrong: %v", blocked)
	}
	if strings.Count(joined, machinecontract.RemediationCommandScopeAcknowledge) != 1 {
		t.Fatalf("duplicate findings duplicated a command: %v", blocked)
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
